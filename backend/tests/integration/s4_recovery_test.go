//go:build integration

package integration_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

// This is a local recovery rehearsal, never a production backup command.
// It accepts only the test runner's already-owned ephemeral container and DB.
func exerciseS4Recovery(t *testing.T, ctx context.Context, source *pgxpool.Pool, sourceDSN string) {
	t.Helper()
	nonce:=os.Getenv("MENDER_TEST_CONTAINER_NONCE")
	container:=os.Getenv("MENDER_TEST_OWNED_CONTAINER")
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(nonce) || container!="mender-pg-test-"+nonce {t.Fatal("recovery requires a runner-owned ephemeral container")}
	u,err:=url.Parse(sourceDSN);must(t,err)
	if ip:=net.ParseIP(u.Hostname());ip==nil || !ip.IsLoopback(){t.Fatal("recovery requires loopback database")}
	sourceName:=strings.TrimPrefix(u.Path,"/")
	if !regexp.MustCompile(`^mender_test_[a-f0-9]{24,32}$`).MatchString(sourceName){t.Fatal("recovery refuses non-test database")}
	docker,err:=exec.LookPath("docker");must(t,err)
	run:=func(c context.Context,args ...string)string{
		cmd:=exec.CommandContext(c,docker,args...)
		out,e:=cmd.CombinedOutput()
		if e!=nil{t.Fatal("owned-container recovery command failed; no database credentials logged",e)}
		return strings.TrimSpace(string(out))
	}
	label:=run(ctx,"inspect","--format",`{{ index .Config.Labels "com.mender.integration-run" }}`,container)
	if label!=nonce{t.Fatal("recovery container ownership mismatch")}
	port:=run(ctx,"port",container,"5432/tcp")
	if port!="127.0.0.1:"+u.Port(){t.Fatal("recovery container/database endpoint mismatch")}
	if got:=run(ctx,"exec",container,"psql","-U","postgres","-d",sourceName,"-Atqc","SELECT current_database()");got!=sourceName{t.Fatal("recovery source mismatch")}
	target:=sourceName+"_restore"
	run(ctx,"exec",container,"createdb","-U","postgres",target)
	t.Cleanup(func(){
		cleanup,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
		run(cleanup,"exec",container,"dropdb","--if-exists","--force","-U","postgres",target)
	})
	backup:="/tmp/mender-s4-"+nonce+".dump"
	started:=time.Now()
	run(ctx,"exec",container,"pg_dump","-U","postgres","--format=custom","--no-owner","--no-acl","--file",backup,sourceName)
	run(ctx,"exec",container,"pg_restore","-U","postgres","--exit-on-error","--no-owner","--no-acl","--dbname",target,backup)
	u.Path="/"+target
	restored,err:=database.Open(ctx,u.String());must(t,err);defer restored.Close()
	must(t,migrations.Verify(ctx,restored))
	tables:=[]string{"execution.runs","execution.run_admissions","execution.jobs","commerce.budget_periods","commerce.billing_journals","commerce.billing_entries","commerce.payment_intents","mender_meta.schema_migrations"}
	for _,table:=range tables{
		query:=`SELECT count(*),md5(coalesce(string_agg(row_to_json(t)::text,E'\n' ORDER BY row_to_json(t)::text),'')) FROM `+table+` t`
		var a,b int64;var ha,hb string
		must(t,source.QueryRow(ctx,query).Scan(&a,&ha));must(t,restored.QueryRow(ctx,query).Scan(&b,&hb))
		if a!=b || ha!=hb{t.Fatalf("restored facts differ: %s",table)}
	}
	// Source and target jobs remain inert. No worker or PSP is started here;
	// restored unknown tasks must be reconciled before operators resume execution.
	t.Logf("PASS: full test database pg_dump/pg_restore; %d tables reconciled; elapsed=%s; no worker restart; no production RPO/RTO claim",len(tables),time.Since(started))
}
