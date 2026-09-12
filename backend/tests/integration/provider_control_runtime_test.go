//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func openTemporaryRoleWithDSN(t *testing.T, ctx context.Context, owner *pgxpool.Pool, baseDSN, prefix string, grant func(context.Context, *pgxpool.Pool, string) error) (*pgxpool.Pool, string) {
	t.Helper()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	role := prefix + suffix
	_, err = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = owner.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = owner.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
	})
	must(t, grant(ctx, owner, role))
	u, err := url.Parse(baseDSN)
	must(t, err)
	u.User = url.UserPassword(role, password)
	pool, err := database.Open(ctx, u.String())
	must(t, err)
	t.Cleanup(pool.Close)
	return pool, u.String()
}

func exerciseProviderHTTPControlRuntime(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_control_worker_", migrations.GrantWorker)
	executorPool, executorDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_control_executor_", migrations.GrantExecutor)
	reconcilerPool, reconcilerDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_control_reconciler_", migrations.GrantReconciler)
	cancelPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_control_cancel_", migrations.GrantCancellation)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.ExecutorRole(ctx, executorPool))
	must(t, database.ReconcilerRole(ctx, reconcilerPool))
	must(t, database.CancellationRole(ctx, cancelPool))

	var statusCalls, cancelCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer integration-secret-value" {
			t.Errorf("provider control did not use current Broker secret: %q", r.Header.Get("Authorization"))
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "ws_control_runtime") || strings.Contains(string(body), "integration-secret-value") {
			t.Errorf("provider control leaked local execution/secret data: %s", body)
		}
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Error(err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status":
			statusCalls.Add(1)
			if envelope["provider_request_id"] != "provider/request-control-status" || r.Header.Get("Idempotency-Key") != "" {
				t.Errorf("unexpected status request: body=%s key=%q", body, r.Header.Get("Idempotency-Key"))
			}
			observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
			_, _ = io.WriteString(w, `{"observation_id":"obs.control.runtime.success","state":"succeeded","result":{"runtime":true},"observed_at":"`+observed+`"}`)
		case "/cancel":
			cancelCalls.Add(1)
			if envelope["provider_request_id"] != "provider/request-control-cancel" || r.Header.Get("Idempotency-Key") != "mender.cancel.run_control_cancel.1" || envelope["cancel_key"] != "mender.cancel.run_control_cancel.1" {
				t.Errorf("unexpected cancel request: body=%s key=%q", body, r.Header.Get("Idempotency-Key"))
			}
			observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
			_, _ = io.WriteString(w, `{"disposition":"acknowledged","observation_id":"obs.control.runtime.cancel","observed_at":"`+observed+`"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	must(t, err)

	// Keep connection/grant validity relative to the actual integration run.
	// A fixed wall-clock fixture can silently expire and make the Provider
	// control path fail at Broker credential revalidation before any HTTP call.
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,cancel_endpoint_url,cancel_http_method,auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
VALUES('deploy_control_runtime','provider_control_runtime','http',$1,'POST',$2,'POST',$3,'POST','bearer',NULL,'Idempotency-Key',1000,4096,8192,'active',$4)`, server.URL+"/submit", server.URL+"/status", server.URL+"/cancel", base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
VALUES('ws_control_runtime','conn_control_runtime','provider_control_runtime','credv_control_runtime','active',1,$1,$2)`, base, base.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
VALUES('ws_control_runtime','conn_control_runtime','sa_control_runtime',true,$1,$2)`, base, base.Add(time.Hour))
	must(t, err)

	clock := &integrationWorkerClock{at: base.Add(time.Second)}
	workerControl, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), clock)
	must(t, err)
	seedAccepted := func(runID, requestID string) runapp.SubmissionRecord {
		_, err := owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_control_runtime',$1,'queued',1,$2,$2)`, runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_control_runtime',$1,'sa_control_runtime','key_control_runtime',$1||'_idem',repeat('d',64),'res_'||$1,'tool_control_runtime','set_control_runtime','conn_control_runtime','price_control_runtime','deploy_control_runtime','budget_control_runtime','period_control_runtime','USD',0,'{"never":"sent by control"}',$2)`, runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,created_at,updated_at) VALUES('ws_control_runtime',$1,'blocked','executor_not_configured',$2,$2,$2)`, runID, base)
		must(t, err)
		count, err := workerControl.Activate(ctx, "ws_control_runtime", []string{"deploy_control_runtime"}, 1)
		must(t, err)
		if count != 1 {
			t.Fatal("provider-control fixture did not activate", runID, count)
		}
		lease, err := workerControl.LeaseOne(ctx, "ws_control_runtime", "worker_control_runtime", 30*time.Second)
		must(t, err)
		clock.at = clock.at.Add(time.Second)
		intent, err := workerControl.BeginSubmission(ctx, lease, "submit."+runID+".1")
		must(t, err)
		clock.at = clock.at.Add(time.Second)
		record, err := workerControl.RecordSubmitted(ctx, intent, "provider_control_runtime", requestID, "task-"+runID)
		must(t, err)
		return record
	}

	secrets := &integrationSecretProvider{}
	controlRuntime, closeControl, err := bootstrap.BuildProviderHTTPControlRuntime(ctx, bootstrap.ProviderHTTPControlRuntimeConfig{
		ExecutorDatabaseURL: executorDSN, ReconcilerDatabaseURL: reconcilerDSN,
		ProviderIDs: []string{"provider_control_runtime"}, AllowedHosts: []string{u.Hostname()}, AllowHTTP: true, AllowLoopback: true,
	}, secrets)
	must(t, err)
	defer closeControl()
	services, err := bootstrap.NewReviewedWorkerServices(nil, controlRuntime, nil)
	must(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	statusSubmission := seedAccepted("run_control_status", "provider/request-control-status")
	cycle, err := bootstrap.RunReviewedRuntimeCycle(ctx, logger, []rundomain.WorkspaceID{"ws_control_runtime"}, services)
	must(t, err)
	if cycle.ProviderControlCycles != 1 || cycle.ProviderCancellationsHandled != 0 || cycle.ProviderReconciliationsHandled != 1 || cycle.SettlementsHandled != 0 || statusCalls.Load() != 1 || cancelCalls.Load() != 0 {
		t.Fatal("status control cycle did not use the bounded reviewed path", cycle, statusCalls.Load(), cancelCalls.Load())
	}
	var runState, jobState string
	must(t, owner.QueryRow(ctx, `SELECT r.state,j.state FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) WHERE r.workspace_id='ws_control_runtime' AND r.id='run_control_status'`).Scan(&runState, &jobState))
	if runState != "succeeded" || jobState != "finished" || statusSubmission.Attempt.ProviderID != "provider_control_runtime" {
		t.Fatal("status control did not converge terminal state", runState, jobState, statusSubmission.Attempt.ProviderID)
	}

	cancelSubmission := seedAccepted("run_control_cancel", "provider/request-control-cancel")
	requestClock := &integrationWorkerClock{at: cancelSubmission.Attempt.SubmittedAt.Add(time.Second)}
	requests, err := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), requestClock)
	must(t, err)
	_, err = requests.Request(ctx, ports.Caller{WorkspaceID: "ws_control_runtime", SubjectID: "sa_control_runtime", CredentialID: "key_control_runtime"}, "run_control_cancel", "stop")
	must(t, err)
	cycle, err = bootstrap.RunReviewedRuntimeCycle(ctx, logger, []rundomain.WorkspaceID{"ws_control_runtime"}, services)
	must(t, err)
	if cycle.ProviderControlCycles != 1 || cycle.ProviderCancellationsHandled != 1 || cycle.ProviderReconciliationsHandled != 0 || cycle.SettlementsHandled != 0 || cancelCalls.Load() != 1 || statusCalls.Load() != 1 {
		t.Fatal("cancel control cycle did not converge exactly once", cycle, statusCalls.Load(), cancelCalls.Load())
	}
	var cancelState string
	must(t, owner.QueryRow(ctx, `SELECT r.state,j.state,c.state FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) JOIN execution.provider_cancel_intents c ON (c.workspace_id,c.run_id)=(r.workspace_id,r.id) WHERE r.workspace_id='ws_control_runtime' AND r.id='run_control_cancel'`).Scan(&runState, &jobState, &cancelState))
	if runState != "canceled" || jobState != "finished" || cancelState != "fulfilled" {
		t.Fatal("cancel control did not converge terminal bundle", runState, jobState, cancelState)
	}
	if secrets.calls != 2 {
		t.Fatal("provider control did not resolve one current secret per network operation", secrets.calls)
	}

	t.Log("real PostgreSQL + local HTTP provider-control runtime verified: separate executor/reconciler roles, broker secret boundary, status/cancel convergence, fixed endpoints and bounded cycle")
}
