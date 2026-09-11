//go:build integration

package integration_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func TestArtifactMigrationBackfillsHistoricalSucceededResult(t *testing.T) {
	dsn := os.Getenv("MENDER_TEST_DATABASE_URL")
	if dsn == "" || os.Getenv("MENDER_TEST_ALLOW_CREATE_DATABASE") != "true" {
		t.Fatal("Artifact migration backfill requires the explicit isolated PostgreSQL test environment")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatal("invalid PostgreSQL test URL")
	}
	ip := net.ParseIP(u.Hostname())
	if !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("Artifact migration backfill only runs against loopback PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := database.Open(ctx, dsn)
	must(t, err)
	defer admin.Close()
	_, suffix, _, err := (keycodec.Codec{}).Generate()
	must(t, err)
	dbname := "mender_artifact_upgrade_" + suffix
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbname}.Sanitize())
	must(t, err)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, e := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{dbname}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Error("Artifact upgrade database cleanup failed")
		}
	}()
	target := *u
	target.Path = "/" + dbname
	owner, err := database.Open(ctx, target.String())
	must(t, err)
	defer owner.Close()
	must(t, migrations.ApplyThroughForIntegration(ctx, owner, "0015_terminal_settlement_jobs.sql"))

	created := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	submitted := created.Add(10 * time.Second)
	terminal := submitted.Add(10 * time.Second)
	tx, err := owner.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES('ws_upgrade','run_upgrade','succeeded',2,$1,$2)`, created, terminal)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
VALUES('ws_upgrade','run_upgrade','sa_upgrade','key_upgrade','upgrade_idem',repeat('a',64),'res_upgrade','tool_upgrade','set_upgrade','conn_upgrade','price_upgrade','deploy_upgrade','budget_upgrade','period_upgrade','USD',0,'{}',$1)`, created)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,stopped_at,available_at,priority,lease_owner,lease_until,lease_generation,attempt_count,max_attempts,updated_at,created_at)
VALUES('ws_upgrade','run_upgrade','finished',NULL,$2,$1,0,NULL,NULL,1,1,3,$2,$1)`, created, terminal)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO execution.run_attempts(workspace_id,run_id,attempt_no,lease_generation,lease_owner,state,leased_at,lease_until,finished_at,submission_key,submission_intent_at,provider_id,provider_request_id,external_task_id,submitted_at,unknown_at,unknown_reason)
VALUES('ws_upgrade','run_upgrade',1,1,'worker_upgrade','submitted',$1,$2,NULL,'upgrade.submit.1',$3,'provider_upgrade','request/upgrade','task-upgrade',$4,NULL,NULL)`, created, created.Add(time.Minute), created.Add(time.Second), submitted)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO execution.provider_observations(workspace_id,run_id,observation_id,attempt_no,provider_id,provider_request_id,external_task_id,state,result_json,error_code,observed_at)
VALUES('ws_upgrade','run_upgrade','obs_upgrade',1,'provider_upgrade','request/upgrade','task-upgrade','succeeded','{"legacy":true}',NULL,$1)`, terminal)
	must(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO execution.settlement_jobs(workspace_id,run_id,observation_id,state,created_at) VALUES('ws_upgrade','run_upgrade','obs_upgrade','pending',$1)`, terminal)
	must(t, err)
	must(t, tx.Commit(ctx))

	var before int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname='artifacts'`).Scan(&before); err != nil || before != 0 {
		t.Fatal("Artifact table existed before 0016", before, err)
	}
	must(t, migrations.Apply(ctx, owner))
	must(t, migrations.Verify(ctx, owner))
	var artifactID, content, source string
	must(t, owner.QueryRow(ctx, `SELECT id,content_json::text,source_observation_id FROM execution.artifacts WHERE workspace_id='ws_upgrade' AND run_id='run_upgrade'`).Scan(&artifactID, &content, &source))
	if artifactID != "art_run_upgrade" || source != "obs_upgrade" || content != `{"legacy": true}` && content != `{"legacy":true}` {
		t.Fatal("0016 did not backfill exact historical result", artifactID, source, content)
	}
}
