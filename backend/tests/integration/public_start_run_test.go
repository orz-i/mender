//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func exercisePublicStartRun(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN, createKey, readOnlyKey string) {
	t.Helper()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	role := "mender_start_" + suffix
	_, err = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := owner.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("public StartRun role grants cleanup failed")
		}
		if _, e := owner.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("public StartRun role cleanup failed")
		}
	}()
	must(t, migrations.GrantAdmission(ctx, owner, role))
	u, err := url.Parse(runtimeDSN)
	must(t, err)
	u.User = url.UserPassword(role, password)
	writerDSN := u.String()
	writer, err := database.Open(ctx, writerDSN)
	must(t, err)
	defer writer.Close()
	must(t, database.AdmissionRole(ctx, writer))

	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,input_schema,state,published_at) VALUES('tool_public_v1','tool_public','1.0.0','provider_public','price_public_v1','deploy_public_v1','{"type":"object","additionalProperties":false,"required":["query","n"],"properties":{"query":{"type":"string","minLength":1},"n":{"type":"integer"}}}'::jsonb,'published',$1)`, at.Add(-time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,state,published_at) VALUES('ws_a','set_public_v1','tool_public','1.0.0','tool_public_v1','budget_public','published',$1)`, at.Add(-time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES('ws_a','conn_public','provider_public','secret_version_internal','active',1,$1,$2)`, at.Add(-time.Hour), at.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES('ws_a','conn_public','sa_a',true,$1,$2)`, at.Add(-time.Hour), at.Add(45*time.Minute))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active) VALUES('price_public_v1','tool_public_v1','USD',40,40,'fixed_success_only',$1,$2,true)`, at.Add(-time.Hour), at.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_a','budget_public','period_public','USD',$1,$2,1000)`, at.Add(-time.Hour), at.Add(time.Hour))
	must(t, err)

	h, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, StartRunAPIEnabled: true, DatabaseURL: runtimeDSN, AdmissionDatabaseURL: writerDSN})
	must(t, err)
	defer closeAPI()
	body := `{"tool_ref":{"tool_id":"tool_public","version":"1.0.0"},"toolset_id":"set_public_v1","connection_id":"conn_public","arguments":{"query":"public-start-secret-marker","n":9007199254740993},"max_charge":{"currency":"USD","amount_micro":"100"}}`
	request := func(key, idem, raw string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws_a/runs", strings.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Idempotency-Key", idem)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	invalidBusiness := strings.Replace(body, `"query":"public-start-secret-marker"`, `"query":42`, 1)
	if w := request(createKey, "public_start_schema_invalid", invalidBusiness); w.Code != http.StatusBadRequest {
		t.Fatal("REST StartRun accepted arguments outside the published ToolVersion schema", w.Code, w.Body.String())
	}
	var invalidAdmissions int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_admissions WHERE workspace_id='ws_a' AND idempotency_key='public_start_schema_invalid'`).Scan(&invalidAdmissions))
	if invalidAdmissions != 0 {
		t.Fatal("schema-invalid REST StartRun created durable admission state", invalidAdmissions)
	}
	var heldBefore int64
	must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='budget_public' AND period_id='period_public'`).Scan(&heldBefore))
	if heldBefore != 0 {
		t.Fatal("schema-invalid REST StartRun reserved quota", heldBefore)
	}
	first := request(createKey, "public_start_0001", body)
	if first.Code != http.StatusAccepted {
		t.Fatal("public admission failed", first.Code, first.Body.String())
	}
	var envelope struct {
		Data struct {
			RunID          string `json:"run_id"`
			ExecutionState string `json:"execution_state"`
			BillingState   string `json:"billing_state"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(first.Body.Bytes(), &envelope))
	if envelope.Data.RunID == "" || envelope.Data.ExecutionState != "queued" || envelope.Data.BillingState != "reserved" {
		t.Fatal("invalid StartRun response", first.Body.String())
	}
	runID := envelope.Data.RunID

	var toolVersion, toolsetVersion, connectionID, priceVersion, deployment, budgetID, periodID, canonical string
	var reserved int64
	must(t, owner.QueryRow(ctx, `SELECT tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,reserved_micro,canonical_arguments FROM execution.run_admissions WHERE workspace_id='ws_a' AND run_id=$1`, runID).Scan(&toolVersion, &toolsetVersion, &connectionID, &priceVersion, &deployment, &budgetID, &periodID, &reserved, &canonical))
	if toolVersion != "tool_public_v1" || toolsetVersion != "set_public_v1" || connectionID != "conn_public" || priceVersion != "price_public_v1" || deployment != "deploy_public_v1" || budgetID != "budget_public" || periodID != "period_public" || reserved != 40 || !strings.Contains(canonical, "9007199254740993") {
		t.Fatal("public admission did not snapshot authoritative plan", toolVersion, toolsetVersion, connectionID, priceVersion, deployment, budgetID, periodID, reserved, canonical)
	}
	var held int64
	must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='budget_public' AND period_id='period_public'`).Scan(&held))
	if held != 40 {
		t.Fatal("public admission did not reserve atomically", held)
	}
	var jobState, blockedReason string
	must(t, owner.QueryRow(ctx, `SELECT state,blocked_reason FROM execution.jobs WHERE workspace_id='ws_a' AND run_id=$1`, runID).Scan(&jobState, &blockedReason))
	if jobState != "blocked" || blockedReason != "executor_not_configured" {
		t.Fatal("public StartRun created runnable work before executor exists", jobState, blockedReason)
	}
	var payload string
	must(t, owner.QueryRow(ctx, `SELECT payload::text FROM execution.outbox WHERE workspace_id='ws_a' AND run_id=$1`, runID).Scan(&payload))
	if strings.Contains(payload, "public-start-secret-marker") {
		t.Fatal("Outbox leaked arguments")
	}

	replay := request(createKey, "public_start_0001", body)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), runID) {
		t.Fatal("public StartRun replay failed", replay.Code, replay.Body.String())
	}
	changed := strings.Replace(body, `"amount_micro":"100"`, `"amount_micro":"101"`, 1)
	if w := request(createKey, "public_start_0001", changed); w.Code != http.StatusConflict {
		t.Fatal("same idempotency key accepted changed request", w.Code, w.Body.String())
	}
	if w := request(readOnlyKey, "public_start_readonly", body); w.Code != http.StatusForbidden {
		t.Fatal("read-only credential admitted a Run", w.Code, w.Body.String())
	}

	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='revoked' WHERE workspace_id='ws_a' AND id='conn_public'`)
	must(t, err)
	if w := request(createKey, "public_start_revoked", body); w.Code != http.StatusForbidden {
		t.Fatal("revoked connection admitted a Run", w.Code, w.Body.String())
	}
	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='active' WHERE workspace_id='ws_a' AND id='conn_public'`)
	must(t, err)

	for _, sql := range []string{
		`SELECT credential_version_ref FROM connections.connections`,
		`SELECT limit_micro FROM commerce.budget_periods`,
		`UPDATE catalog.tool_versions SET state='disabled'`,
	} {
		if _, err = runtime.Exec(ctx, sql); err == nil {
			t.Fatal("runtime role exceeded plan read contract", sql)
		}
	}
	for _, sql := range []string{
		`SELECT * FROM catalog.tool_versions`,
		`SELECT * FROM distribution.toolset_bindings`,
		`SELECT * FROM connections.connections`,
		`SELECT * FROM commerce.price_versions`,
	} {
		if _, err = writer.Exec(ctx, sql); err == nil {
			t.Fatal("admission writer read plan source", sql)
		}
	}
	if h, closeWrong, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, StartRunAPIEnabled: true, DatabaseURL: runtimeDSN, AdmissionDatabaseURL: runtimeDSN}); err == nil || h != nil {
		if closeWrong != nil {
			closeWrong()
		}
		t.Fatal("reader role reused as admission writer")
	}
}
