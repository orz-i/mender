package postgres

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config validates all fallback hosts too. Non-loopback TCP requires verified TLS.
func Config(dsn string) (*pgxpool.Config, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("database URL is required")
	}
	c, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid database configuration (details redacted)")
	}
	local := func(host string) bool {
		ip := net.ParseIP(host)
		return strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	}
	if !local(c.ConnConfig.Host) && (c.ConnConfig.TLSConfig == nil || c.ConnConfig.TLSConfig.InsecureSkipVerify) {
		return nil, errors.New("non-loopback database requires verified TLS")
	}
	for _, f := range c.ConnConfig.Fallbacks {
		if !local(f.Host) && (f.TLSConfig == nil || f.TLSConfig.InsecureSkipVerify) {
			return nil, errors.New("insecure database fallback is not allowed")
		}
	}
	c.MaxConns = 4
	c.MinConns = 0
	c.ConnConfig.ConnectTimeout = 3 * time.Second
	c.ConnConfig.RuntimeParams["application_name"] = "mender"
	c.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	c.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	return c, nil
}

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	c, err := Config(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		return nil, errors.New("database connection failed (details redacted)")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("database unavailable (details redacted)")
	}
	return pool, nil
}

// RuntimeRole rejects owner/elevated roles; RLS is defense-in-depth, not authentication.
func RuntimeRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))
	 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN ('identity','execution','mender_meta') AND c.relkind='r' AND pg_has_role(current_user,c.relowner,'MEMBER'))
	 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN ('identity','execution','mender_meta') AND pg_has_role(current_user,n.nspowner,'MEMBER'))`).Scan(&unsafe)
	if err != nil || unsafe {
		return errors.New("API database role is privileged or owns protected tables")
	}
	var rls int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname IN ('runs','run_events') AND c.relrowsecurity AND c.relforcerowsecurity").Scan(&rls); err != nil || rls != 2 {
		return errors.New("execution RLS safeguards missing")
	}
	var grants bool
	err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'identity.api_keys','SELECT')
	 AND has_table_privilege(current_user,'identity.workspaces','SELECT') AND has_table_privilege(current_user,'identity.service_accounts','SELECT')
	 AND has_table_privilege(current_user,'execution.runs','SELECT') AND has_column_privilege(current_user,'execution.runs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','version','UPDATE') AND has_column_privilege(current_user,'execution.runs','updated_at','UPDATE')
	 AND has_table_privilege(current_user,'execution.run_events','INSERT')
	 AND has_table_privilege(current_user,'execution.run_events','SELECT')
	 AND NOT has_schema_privilege(current_user,'identity','CREATE')
	 AND NOT has_schema_privilege(current_user,'execution','CREATE')
	 AND NOT has_schema_privilege(current_user,'mender_meta','CREATE')
	 AND NOT has_table_privilege(current_user,'identity.api_keys','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'identity.workspaces','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'identity.service_accounts','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_any_column_privilege(current_user,'identity.api_keys','INSERT,UPDATE')
	 AND NOT has_any_column_privilege(current_user,'identity.workspaces','INSERT,UPDATE')
	 AND NOT has_any_column_privilege(current_user,'identity.service_accounts','INSERT,UPDATE')
	 AND NOT has_table_privilege(current_user,'mender_meta.schema_migrations','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_any_column_privilege(current_user,'mender_meta.schema_migrations','INSERT,UPDATE')
	 AND NOT has_table_privilege(current_user,'execution.runs','INSERT,DELETE,TRUNCATE')
	 AND NOT has_any_column_privilege(current_user,'execution.runs','INSERT')
	 AND NOT has_column_privilege(current_user,'execution.runs','id','UPDATE')
	 AND NOT has_column_privilege(current_user,'execution.runs','workspace_id','UPDATE')
	 AND NOT has_column_privilege(current_user,'execution.runs','created_at','UPDATE')
	 AND NOT has_table_privilege(current_user,'execution.run_events','UPDATE,DELETE,TRUNCATE')
	 AND NOT has_any_column_privilege(current_user,'execution.run_events','UPDATE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("API database grants do not match the restricted runtime contract")
	}
	if err = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.run_admissions','workspace_id','SELECT') AND has_column_privilege(current_user,'execution.run_admissions','run_id','SELECT') AND NOT has_column_privilege(current_user,'execution.run_admissions','canonical_arguments','SELECT') AND NOT has_schema_privilege(current_user,'commerce','CREATE')`).Scan(&grants); err != nil || !grants {
		return errors.New("API admission isolation grants are invalid")
	}
	for _, table := range []string{"commerce.budget_periods", "commerce.reservations", "execution.run_admissions", "execution.jobs", "execution.outbox"} {
		if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,c.oid,'INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,c.oid,'INSERT,UPDATE') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname=$1`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("query API cannot write admission or quota tables")
		}
	}
	return nil
}
