//go:build integration

package integration_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	callbacksecret "github.com/orz-i/mender/backend/internal/processes/providercallback/adapters/outbound/filesecret"
	"github.com/orz-i/mender/backend/migrations"
)

func callbackSignature(secret []byte, timestamp, body string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("mender-callback-v1\n" + timestamp + "\n" + body))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func callbackBody(eventID, runID, providerRequestID, externalTaskID, observationID, state, result, errorCode string, observedAt time.Time) string {
	if result == "" {
		result = "null"
	}
	errorValue := "null"
	if errorCode != "" {
		errorValue = strconv.Quote(errorCode)
	}
	externalValue := "null"
	if externalTaskID != "" {
		externalValue = strconv.Quote(externalTaskID)
	}
	return fmt.Sprintf(`{"schema_version":1,"event_type":"provider.observation","event_id":%s,"workspace_id":"ws_callback","run_id":%s,"attempt_no":1,"provider_request_id":%s,"external_task_id":%s,"observation_id":%s,"state":%s,"result":%s,"error_code":%s,"occurred_at":%s}`,
		strconv.Quote(eventID), strconv.Quote(runID), strconv.Quote(providerRequestID), externalValue, strconv.Quote(observationID), strconv.Quote(state), result, errorValue, strconv.Quote(observedAt.UTC().Format(time.RFC3339Nano)))
}

func exerciseProviderCallbackRuntime(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, _ := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_callback_worker_", migrations.GrantWorker)
	callbackPool, callbackDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_callback_ingestor_", migrations.GrantCallbackIngestor)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.CallbackIngestorRole(ctx, callbackPool))
	probe, err := callbackPool.Begin(ctx)
	must(t, err)
	_, err = probe.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_callback',true)`)
	must(t, err)
	_, err = probe.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(42))
	if err != nil {
		_ = probe.Rollback(ctx)
		t.Fatal("callback role cannot acquire reviewed advisory event lock", err)
	}
	rows, err := probe.Query(ctx, `SELECT receipt_id FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' FOR UPDATE`)
	if err != nil {
		_ = probe.Rollback(ctx)
		t.Fatal("callback role cannot lock Inbox rows", err)
	}
	rows.Close()
	_, err = probe.Exec(ctx, `INSERT INTO execution.provider_callback_inbox(receipt_id,workspace_id,provider_id,event_id,body_sha256,key_id,signed_at,received_at,last_received_at,delivery_count,run_id,attempt_no,provider_request_id,external_task_id,observation_id,observation_state,observed_at,disposition,reason_code,processed_at)
VALUES(repeat('a',64),'ws_callback','provider_callback','evt.probe',repeat('b',64),'key_current',clock_timestamp(),clock_timestamp(),clock_timestamp(),1,'run_probe',1,'request/probe',NULL,'obs.probe','pending',clock_timestamp(),'quarantined','probe',clock_timestamp())`)
	if err != nil {
		_ = probe.Rollback(ctx)
		t.Fatal("callback role cannot insert bounded Inbox metadata", err)
	}
	must(t, probe.Rollback(ctx))

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	clock := &integrationWorkerClock{at: base.Add(10 * time.Second)}
	control, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), clock)
	must(t, err)
	seedSubmitted := func(runID string) runapp.SubmissionRecord {
		_, e := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_callback',$1,'queued',1,$2,$2)`, runID, base)
		must(t, e)
		_, e = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_callback',$1,'sa_callback','key_callback',$1||'_idem',repeat('c',64),'res_'||$1,'tool_callback','set_callback','conn_callback','price_callback','deploy_callback','budget_callback','period_callback','USD',0,'{}',$2)`, runID, base)
		must(t, e)
		_, e = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,priority,created_at,updated_at) VALUES('ws_callback',$1,'blocked','executor_not_configured',$2,10,$2,$2)`, runID, base)
		must(t, e)
		count, e := control.Activate(ctx, "ws_callback", []string{"deploy_callback"}, 1)
		must(t, e)
		if count != 1 {
			t.Fatal("callback fixture did not activate", runID, count)
		}
		lease, e := control.LeaseOne(ctx, "ws_callback", "worker_"+runID, 30*time.Second)
		must(t, e)
		clock.at = clock.at.Add(time.Second)
		intent, e := control.BeginSubmission(ctx, lease, "submit."+runID+".1")
		must(t, e)
		clock.at = clock.at.Add(time.Second)
		record, e := control.RecordSubmitted(ctx, intent, "provider_callback", "request/"+runID, "task/"+runID)
		must(t, e)
		return record
	}

	success := seedSubmitted("run_callback_success")
	binding := seedSubmitted("run_callback_binding")
	order := seedSubmitted("run_callback_order")

	secret := []byte(strings.Repeat("callback-secret-", 3))
	secretRoot := t.TempDir()
	secretName, err := callbacksecret.FileName("provider_callback", "key_current")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(secretRoot, secretName), secret, 0o600))
	h, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{
		RunAPIEnabled: true, DatabaseURL: runtimeDSN,
		ProviderCallbackEnabled: true, ProviderCallbackDatabaseURL: callbackDSN,
		ProviderCallbackSecretRoot: secretRoot, ProviderCallbackReviewedKeys: map[string][]string{"provider_callback": {"key_current"}},
	})
	must(t, err)
	defer closeAPI()

	post := func(body string) *httptest.ResponseRecorder {
		timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, "/api/provider-callbacks/v1/providers/provider_callback", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Mender-Callback-Key-Id", "key_current")
		req.Header.Set("X-Mender-Callback-Timestamp", timestamp)
		req.Header.Set("X-Mender-Callback-Signature", callbackSignature(secret, timestamp, body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	successObserved := success.Attempt.SubmittedAt.Add(5 * time.Second)
	successBody := callbackBody("evt.callback.success", "run_callback_success", "request/run_callback_success", "task/run_callback_success", "obs.callback.success", "succeeded", `{"answer":42}`, "", successObserved)
	w := post(successBody)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"disposition":"accepted"`) {
		var inboxCount, observationCount, artifactCount int
		var inboxState string
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.success'`).Scan(&inboxCount))
		if inboxCount == 1 {
			must(t, owner.QueryRow(ctx, `SELECT disposition FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.success'`).Scan(&inboxState))
		}
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_observations WHERE workspace_id='ws_callback' AND run_id='run_callback_success'`).Scan(&observationCount))
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts WHERE workspace_id='ws_callback' AND run_id='run_callback_success'`).Scan(&artifactCount))
		t.Fatal("success callback response", w.Code, w.Body.String(), "inbox", inboxCount, inboxState, "observations", observationCount, "artifacts", artifactCount)
	}
	var runState, jobState, disposition string
	var artifactCount, deliveryCount int
	must(t, owner.QueryRow(ctx, `SELECT r.state,j.state FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) WHERE r.workspace_id='ws_callback' AND r.id='run_callback_success'`).Scan(&runState, &jobState))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts WHERE workspace_id='ws_callback' AND run_id='run_callback_success' AND kind='provider_result'`).Scan(&artifactCount))
	must(t, owner.QueryRow(ctx, `SELECT disposition,delivery_count FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.success' AND disposition='accepted'`).Scan(&disposition, &deliveryCount))
	if runState != "succeeded" || jobState != "finished" || artifactCount != 1 || disposition != "accepted" || deliveryCount != 1 {
		t.Fatal("success callback did not converge through existing execution truth", runState, jobState, artifactCount, disposition, deliveryCount)
	}

	w = post(successBody)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"disposition":"duplicate"`) {
		t.Fatal("callback replay was not idempotent", w.Code, w.Body.String())
	}
	must(t, owner.QueryRow(ctx, `SELECT delivery_count FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.success' AND disposition='accepted'`).Scan(&deliveryCount))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts WHERE workspace_id='ws_callback' AND run_id='run_callback_success'`).Scan(&artifactCount))
	if deliveryCount != 2 || artifactCount != 1 {
		t.Fatal("duplicate callback changed execution cardinality", deliveryCount, artifactCount)
	}

	conflictBody := callbackBody("evt.callback.success", "run_callback_success", "request/run_callback_success", "task/run_callback_success", "obs.callback.success.conflict", "succeeded", `{"answer":43}`, "", successObserved.Add(time.Second))
	w = post(conflictBody)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"disposition":"quarantined"`) {
		t.Fatal("event-id conflict was not quarantined", w.Code, w.Body.String())
	}
	var quarantined, observations int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.success' AND disposition='quarantined' AND reason_code='event_id_conflict'`).Scan(&quarantined))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_observations WHERE workspace_id='ws_callback' AND run_id='run_callback_success'`).Scan(&observations))
	if quarantined != 1 || observations != 1 {
		t.Fatal("event-id conflict mutated provider evidence", quarantined, observations)
	}

	terminalReplay := callbackBody("evt.callback.terminal-replay", "run_callback_success", "request/run_callback_success", "task/run_callback_success", "obs.callback.terminal-replay", "succeeded", `{"answer":42}`, "", successObserved.Add(2*time.Second))
	w = post(terminalReplay)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"disposition":"quarantined"`) {
		t.Fatal("terminal callback replay response", w.Code, w.Body.String())
	}
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.terminal-replay' AND disposition='quarantined' AND reason_code='terminal_replay'`).Scan(&quarantined))
	if quarantined != 1 {
		t.Fatal("terminal callback replay was not quarantined", quarantined)
	}

	bindingBody := callbackBody("evt.callback.binding", "run_callback_binding", "request/wrong", "task/run_callback_binding", "obs.callback.binding", "pending", "", "", binding.Attempt.SubmittedAt.Add(time.Second))
	w = post(bindingBody)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"disposition":"quarantined"`) {
		t.Fatal("binding mismatch response", w.Code, w.Body.String())
	}
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.binding' AND disposition='quarantined' AND reason_code='attempt_binding_mismatch'`).Scan(&quarantined))
	must(t, owner.QueryRow(ctx, `SELECT state FROM execution.runs WHERE workspace_id='ws_callback' AND id='run_callback_binding'`).Scan(&runState))
	if quarantined != 1 || runState != "running" {
		t.Fatal("binding mismatch advanced Run", quarantined, runState)
	}

	orderNewAt := order.Attempt.SubmittedAt.Add(5 * time.Second)
	orderNew := callbackBody("evt.callback.order-new", "run_callback_order", "request/run_callback_order", "task/run_callback_order", "obs.callback.order-new", "pending", "", "", orderNewAt)
	if w = post(orderNew); w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"disposition":"accepted"`) {
		t.Fatal("newer pending callback failed", w.Code, w.Body.String())
	}
	orderOld := callbackBody("evt.callback.order-old", "run_callback_order", "request/run_callback_order", "task/run_callback_order", "obs.callback.order-old", "pending", "", "", orderNewAt.Add(-time.Second))
	if w = post(orderOld); w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"disposition":"quarantined"`) {
		t.Fatal("older callback was not quarantined", w.Code, w.Body.String())
	}
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_callback_inbox WHERE workspace_id='ws_callback' AND event_id='evt.callback.order-old' AND reason_code='out_of_order'`).Scan(&quarantined))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_observations WHERE workspace_id='ws_callback' AND run_id='run_callback_order'`).Scan(&observations))
	if quarantined != 1 || observations != 1 {
		t.Fatal("out-of-order callback mutated execution evidence", quarantined, observations)
	}

	for _, sql := range []string{
		`SELECT canonical_arguments FROM execution.run_admissions`,
		`SELECT * FROM supply.deployments`,
		`SELECT * FROM identity.api_keys`,
		`UPDATE execution.provider_observations SET state='failed'`,
	} {
		if _, e := callbackPool.Exec(ctx, sql); e == nil {
			t.Fatal("callback-ingestor role accepted forbidden operation", sql)
		}
	}

	t.Log("real PostgreSQL Provider Callback ingress verified: raw-body HMAC, restricted callback role, Inbox replay/conflict quarantine, Attempt ownership binding, ordering checks and existing Run/Artifact convergence")
}
