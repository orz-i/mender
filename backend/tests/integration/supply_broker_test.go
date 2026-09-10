//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	connfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	execfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	execpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	connectioncredentials "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/connectioncredentials"
	executioninput "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/executioninput"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
)

type integrationSecretProvider struct {
	calls int
	last  supplyapp.SecretRequest
}

func (p *integrationSecretProvider) ResolveSecret(_ context.Context, request supplyapp.SecretRequest) (supplyapp.Secret, error) {
	p.calls++
	p.last = request
	return supplyapp.NewSecret([]byte("integration-secret-value"))
}

func exerciseSupplyBroker(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	role := "mender_executor_" + suffix
	_, err = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := owner.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("executor grants cleanup failed")
		}
		if _, e := owner.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); e != nil {
			t.Error("executor role cleanup failed")
		}
	}()

	var operatorOut, operatorErr bytes.Buffer
	must(t, bootstrap.RunOperator(ctx, []string{"grant-executor", "--role", role}, func(key string) string {
		if key == "MENDER_ADMIN_DATABASE_URL" {
			return owner.Config().ConnString()
		}
		return ""
	}, &operatorOut, &operatorErr))
	if !strings.Contains(operatorOut.String(), "Restricted executor-runtime grants applied") || operatorErr.Len() != 0 || strings.Contains(operatorOut.String(), password) {
		t.Fatal("executor grant output is unsafe or incomplete")
	}

	u, err := url.Parse(runtimeDSN)
	must(t, err)
	u.User = url.UserPassword(role, password)
	executorPool, err := database.Open(ctx, u.String())
	must(t, err)
	defer executorPool.Close()
	must(t, database.ExecutorRole(ctx, executorPool))
	if err = database.ExecutorRole(ctx, owner); err == nil {
		t.Fatal("owner accepted as executor role")
	}
	if err = database.ExecutorRole(ctx, runtime); err == nil {
		t.Fatal("runtime API role accepted as executor role")
	}

	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(revision,provider_id,transport_kind,endpoint_url,http_method,auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
VALUES('deploy_broker','provider_broker','http','https://provider.invalid/v1/run','POST','bearer',NULL,'Idempotency-Key',10000,4096,8192,'active',$1)`, at)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
VALUES('ws_broker','conn_broker','provider_broker','credv_broker','active',3,$1,$2)`, at, at.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
VALUES('ws_broker','conn_broker','sa_broker',true,$1,$2)`, at, at.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_broker','run_broker','queued',1,$1,$1)`, at)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_broker','run_broker','sa_broker','key_broker','idem_broker_12345',repeat('a',64),'res_broker','toolv_broker','set_broker','conn_broker','price_broker','deploy_broker','budget_broker','period_broker','USD',0,'{"message":"hello"}',$1)`, at)
	must(t, err)

	execService, err := execapp.NewRuntimeInputService(execpg.NewRuntimeInputs(executorPool))
	must(t, err)
	connService, err := connapp.NewRuntimeService(connpg.NewRuntimeRepository(executorPool))
	must(t, err)
	secrets := &integrationSecretProvider{}
	broker, err := supplyapp.NewBroker(
		executioninput.New(execfacade.NewRuntimeInputs(execService)),
		connectioncredentials.New(connfacade.NewRuntimeCredentials(connService)),
		supplypg.NewDeployments(executorPool),
		secrets,
	)
	must(t, err)

	prepared, err := broker.Prepare(ctx, supplyapp.InvocationRef{WorkspaceID: "ws_broker", RunID: "run_broker"}, at.Add(10*time.Second))
	must(t, err)
	if prepared.Deployment.Revision != "deploy_broker" || prepared.Deployment.ProviderID != "provider_broker" || prepared.Credential.CredentialVersionRef != "credv_broker" || prepared.CanonicalArguments != `{"message":"hello"}` || string(prepared.Secret.Bytes()) != "integration-secret-value" || secrets.calls != 1 {
		t.Fatal("broker did not assemble authoritative runtime material", prepared, secrets.calls)
	}
	if secrets.last.CredentialVersionRef != "credv_broker" || secrets.last.ConnectionRevision != 3 {
		t.Fatal("secret provider did not receive opaque versioned reference", secrets.last)
	}

	t.Run("executor role is read-only and RLS scoped", func(t *testing.T) {
		for _, sql := range []string{
			`SELECT * FROM identity.api_keys`,
			`SELECT * FROM commerce.budget_periods`,
			`SELECT * FROM catalog.tool_versions`,
			`SELECT * FROM execution.jobs`,
			`UPDATE supply.deployments SET state='disabled'`,
			`UPDATE connections.connections SET state='revoked'`,
			`UPDATE execution.run_admissions SET canonical_arguments='{}'`,
		} {
			if _, e := executorPool.Exec(ctx, sql); e == nil {
				t.Fatal("executor role accepted forbidden operation", sql)
			}
		}
		var count int
		must(t, executorPool.QueryRow(ctx, `SELECT count(*) FROM execution.run_admissions`).Scan(&count))
		if count != 0 {
			t.Fatal("executor role bypassed workspace RLS", count)
		}
	})

	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='revoked' WHERE workspace_id='ws_broker' AND id='conn_broker'`)
	must(t, err)
	beforeSecretCalls := secrets.calls
	_, err = broker.Prepare(ctx, supplyapp.InvocationRef{WorkspaceID: "ws_broker", RunID: "run_broker"}, at.Add(20*time.Second))
	if !errors.Is(err, supplyapp.ErrInvocationForbidden) || secrets.calls != beforeSecretCalls {
		t.Fatal("revoked connection reached secret provider", err, secrets.calls, beforeSecretCalls)
	}

	t.Log("real PostgreSQL supply broker verified: executor role isolation, immutable admitted input, current connection revalidation, opaque credential reference and in-memory secret boundary")
}
