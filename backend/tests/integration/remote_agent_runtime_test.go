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
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	filesecret "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/filesecret"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supplydomain "github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func exerciseRemoteAgentRuntime(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	worker, workerDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_agent_worker_", migrations.GrantWorker)
	executorPool, executorDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_agent_executor_", migrations.GrantExecutor)
	reconcilerPool, reconcilerDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_agent_reconciler_", migrations.GrantReconciler)
	cancelPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_agent_cancel_", migrations.GrantCancellation)
	must(t, database.WorkerRole(ctx, worker))
	must(t, database.ExecutorRole(ctx, executorPool))
	must(t, database.ReconcilerRole(ctx, reconcilerPool))
	must(t, database.CancellationRole(ctx, cancelPool))

	var submitCalls, statusCalls, cancelCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer remote-agent-secret" {
			t.Errorf("remote Agent runtime did not use the mounted current credential: %q", r.Header.Get("Authorization"))
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "remote-agent-secret") || strings.Contains(string(body), "ws_remote_agent") {
			t.Errorf("remote Agent request leaked secret/local workspace facts: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/agent/submit":
			submitCalls.Add(1)
			key := r.Header.Get("Idempotency-Key")
			var args map[string]any
			if err := json.Unmarshal(body, &args); err != nil || args["prompt"] != "ship it" || len(args) != 1 {
				t.Errorf("unexpected Agent submit arguments: %s", body)
			}
			switch key {
			case "mender.submit.run_agent_success.1":
				_, _ = io.WriteString(w, `{"provider_request_id":"agent/request-success","external_task_id":"agent/task-success"}`)
			case "mender.submit.run_agent_cancel.1":
				_, _ = io.WriteString(w, `{"provider_request_id":"agent/request-cancel","external_task_id":"agent/task-cancel"}`)
			default:
				t.Errorf("unexpected Agent submission key: %q", key)
				http.Error(w, "bad key", http.StatusBadRequest)
			}
		case "/agent/status":
			statusCalls.Add(1)
			if r.Header.Get("Idempotency-Key") != "" {
				t.Errorf("status query unexpectedly carried idempotency key: %q", r.Header.Get("Idempotency-Key"))
			}
			var envelope map[string]any
			if err := json.Unmarshal(body, &envelope); err != nil || envelope["provider_request_id"] != "agent/request-success" || envelope["external_task_id"] != "agent/task-success" {
				t.Errorf("unexpected Agent status request: %s", body)
			}
			observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
			_, _ = io.WriteString(w, `{"observation_id":"obs.agent.success","state":"succeeded","result":{"answer":"agent-done"},"observed_at":"`+observed+`"}`)
		case "/agent/cancel":
			cancelCalls.Add(1)
			var envelope map[string]any
			if err := json.Unmarshal(body, &envelope); err != nil || envelope["provider_request_id"] != "agent/request-cancel" || envelope["external_task_id"] != "agent/task-cancel" || envelope["cancel_key"] != "mender.cancel.run_agent_cancel.1" || r.Header.Get("Idempotency-Key") != "mender.cancel.run_agent_cancel.1" {
				t.Errorf("unexpected Agent cancel request: body=%s key=%q", body, r.Header.Get("Idempotency-Key"))
			}
			observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
			_, _ = io.WriteString(w, `{"disposition":"acknowledged","observation_id":"obs.agent.cancel","observed_at":"`+observed+`"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	must(t, err)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES('ws_remote_agent',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(
	 revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,cancel_endpoint_url,cancel_http_method,
	 auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_remote_agent','provider_remote_agent','agent_http',$1,'POST',$2,'POST',$3,'POST','bearer',NULL,'Idempotency-Key',1000,4096,8192,'active',$4)`,
		server.URL+"/agent/submit", server.URL+"/agent/status", server.URL+"/agent/cancel", base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
	 VALUES('ws_remote_agent','conn_remote_agent','provider_remote_agent','credv_remote_agent','active',1,$1,$2)`, base, base.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
	 VALUES('ws_remote_agent','conn_remote_agent','sa_remote_agent',true,$1,$2)`, base, base.Add(time.Hour))
	must(t, err)

	seed := func(runID string, priority int) {
		_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_remote_agent',$1,'queued',1,$2,$2)`, runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(
		 workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,
		 price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
		 VALUES('ws_remote_agent',$1,'sa_remote_agent','key_remote_agent',$1||'_idem',repeat('a',64),'res_'||$1,'tool_remote_agent','set_remote_agent',
		 'conn_remote_agent','price_remote_agent','deploy_remote_agent','budget_remote_agent','period_remote_agent','USD',0,'{"prompt":"ship it"}',$2)`, runID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,available_at,priority,created_at,updated_at)
		 VALUES('ws_remote_agent',$1,'blocked','executor_not_configured',$2,$3,$2,$2)`, runID, base, priority)
		must(t, err)
	}
	seed("run_agent_success", 20)
	seed("run_agent_cancel", 10)

	secretRoot := t.TempDir()
	secretName, err := filesecret.FileName(supplyapp.SecretRequest{
		ProviderID: "provider_remote_agent", ConnectionID: "conn_remote_agent", CredentialVersionRef: "credv_remote_agent", ConnectionRevision: 1,
	})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(secretRoot, secretName), []byte("remote-agent-secret"), 0o600))
	secrets, err := filesecret.New(secretRoot)
	must(t, err)
	agentExecutor, closeExecutor, err := bootstrap.BuildSupplierHTTPExecutor(ctx, bootstrap.SupplierHTTPRuntimeConfig{
		WorkerDatabaseURL: workerDSN, ExecutorDatabaseURL: executorDSN, AllowedHosts: []string{serverURL.Hostname()},
		TransportKinds: []string{supplydomain.TransportAgentHTTP}, AllowHTTP: true, AllowLoopback: true,
	}, secrets)
	must(t, err)
	defer closeExecutor()
	services, closeServices, err := bootstrap.BuildReviewedWorkerServicesFromConfig(ctx, bootstrap.WorkerConfig{
		ControlEnabled: true, DispatchEnabled: true, DatabaseURL: workerDSN, WorkerID: "worker_remote_agent",
		Workspaces: []rundomain.WorkspaceID{"ws_remote_agent"}, DeploymentRevisions: []string{"deploy_remote_agent"},
	}, bootstrap.ReviewedWorkerHostConfig{
		Enabled: true, AgentDispatchEnabled: true, ProviderControlEnabled: true, SecretRoot: secretRoot,
		ExecutorDatabaseURL: executorDSN, ReconcilerDatabaseURL: reconcilerDSN, ProviderIDs: []string{"provider_remote_agent"},
		AllowedHosts: []string{serverURL.Hostname()}, AllowHTTP: true, AllowLoopback: true,
	})
	must(t, err)
	defer closeServices()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	workerClock := &integrationWorkerClock{at: base.Add(time.Second)}
	workerControl, err := runapp.NewWorkerControl(runpg.NewWorkers(worker), workerClock)
	must(t, err)
	dispatcher, err := runapp.NewDispatcher(workerControl, agentExecutor)
	must(t, err)
	dispatch := func(runID string) runapp.DispatchResult {
		t.Helper()
		count, e := workerControl.Activate(ctx, "ws_remote_agent", []string{"deploy_remote_agent"}, 1)
		must(t, e)
		if count != 1 {
			t.Fatal("remote Agent fixture did not activate", runID, count)
		}
		lease, e := workerControl.LeaseOne(ctx, "ws_remote_agent", "worker_remote_agent", 30*time.Second)
		must(t, e)
		workerClock.at = workerClock.at.Add(time.Second)
		result, e := dispatcher.Dispatch(ctx, lease)
		must(t, e)
		if string(result.Run.ID) != runID || result.Run.State != "running" || result.Attempt.State != "submitted" {
			t.Fatal("remote Agent dispatch did not enter existing provider-waiting lifecycle", runID, result)
		}
		workerClock.at = workerClock.at.Add(time.Second)
		return result
	}

	dispatch("run_agent_success")
	var providerID, requestID, taskID, jobState string
	must(t, owner.QueryRow(ctx, `SELECT a.provider_id,a.provider_request_id,a.external_task_id,j.state
	 FROM execution.run_attempts a JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(a.workspace_id,a.run_id)
	 WHERE a.workspace_id='ws_remote_agent' AND a.run_id='run_agent_success' AND a.attempt_no=1`).Scan(&providerID, &requestID, &taskID, &jobState))
	if providerID != "provider_remote_agent" || requestID != "agent/request-success" || taskID != "agent/task-success" || jobState != "provider_waiting" {
		t.Fatal("remote Agent acceptance did not use existing Attempt/Job facts", providerID, requestID, taskID, jobState)
	}
	cycle, err := bootstrap.RunReviewedRuntimeCycle(ctx, logger, []rundomain.WorkspaceID{"ws_remote_agent"}, services)
	must(t, err)
	if cycle.ProviderControlCycles != 1 || cycle.ProviderCancellationsHandled != 0 || cycle.ProviderReconciliationsHandled != 1 || statusCalls.Load() != 1 || cancelCalls.Load() != 0 {
		t.Fatal("remote Agent status convergence did not use reviewed provider control", cycle, statusCalls.Load(), cancelCalls.Load())
	}
	var runState, artifactKind, artifactContent string
	must(t, owner.QueryRow(ctx, `SELECT r.state,j.state,a.kind,a.content_json::text
	 FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id)
	 JOIN execution.artifacts a ON (a.workspace_id,a.run_id)=(r.workspace_id,r.id)
	 WHERE r.workspace_id='ws_remote_agent' AND r.id='run_agent_success'`).Scan(&runState, &jobState, &artifactKind, &artifactContent))
	if runState != "succeeded" || jobState != "finished" || artifactKind != "provider_result" || !strings.Contains(artifactContent, `"agent-done"`) {
		t.Fatal("remote Agent result did not converge through existing Run/Artifact lifecycle", runState, jobState, artifactKind, artifactContent)
	}

	dispatch("run_agent_cancel")
	requestClock := &integrationWorkerClock{at: workerClock.at.Add(time.Second)}
	cancelRequests, err := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), requestClock)
	must(t, err)
	_, err = cancelRequests.Request(ctx, ports.Caller{WorkspaceID: "ws_remote_agent", SubjectID: "sa_remote_agent", CredentialID: "key_remote_agent"}, "run_agent_cancel", "stop")
	must(t, err)
	cycle, err = bootstrap.RunReviewedRuntimeCycle(ctx, logger, []rundomain.WorkspaceID{"ws_remote_agent"}, services)
	must(t, err)
	if cycle.ProviderControlCycles != 1 || cycle.ProviderCancellationsHandled != 1 || cycle.ProviderReconciliationsHandled != 0 || cancelCalls.Load() != 1 || statusCalls.Load() != 1 {
		t.Fatal("remote Agent cancellation did not converge exactly once", cycle, statusCalls.Load(), cancelCalls.Load())
	}
	var cancelState string
	must(t, owner.QueryRow(ctx, `SELECT r.state,j.state,c.state FROM execution.runs r
	 JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id)
	 JOIN execution.provider_cancel_intents c ON (c.workspace_id,c.run_id)=(r.workspace_id,r.id)
	 WHERE r.workspace_id='ws_remote_agent' AND r.id='run_agent_cancel'`).Scan(&runState, &jobState, &cancelState))
	if runState != "canceled" || jobState != "finished" || cancelState != "fulfilled" {
		t.Fatal("remote Agent cancellation did not reuse existing terminal bundle", runState, jobState, cancelState)
	}
	var canceledArtifacts int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts WHERE workspace_id='ws_remote_agent' AND run_id='run_agent_cancel'`).Scan(&canceledArtifacts))
	if canceledArtifacts != 0 {
		t.Fatal("canceled remote Agent Run produced a success Artifact", canceledArtifacts)
	}
	if submitCalls.Load() != 2 {
		t.Fatal("remote Agent submit was retried or skipped unexpectedly", submitCalls.Load())
	}

	t.Log("real PostgreSQL remote Agent runtime verified: reviewed agent_http submit, mounted secret boundary, durable external task, status/result Artifact convergence, cancellation, and existing Run/Job truth")
}
