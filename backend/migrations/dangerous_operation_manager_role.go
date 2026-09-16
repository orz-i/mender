package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GrantDangerousOperationManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid dangerous-operation-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("dangerous-operation-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("dangerous-operation-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA governance,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON governance.dangerous_operation_approvals,governance.dangerous_operation_audit_events,governance.jit_support_grants TO " + id,
		"REVOKE EXECUTE ON FUNCTION governance.submit_dangerous_operation(text,text,text,text,text,text,text,text,text,jsonb,text,bigint,text,text,timestamptz,timestamptz) FROM " + id,
		"GRANT EXECUTE ON FUNCTION governance.request_release_emergency_approval(text,text,text,text,text,timestamptz,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.request_support_jit_approval(text,text,text,text[],integer,text,timestamptz,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.request_commerce_approval(text,text,text,text,text,text,text,text,bigint,text,text,timestamptz,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.approve_dangerous_operation(text,text,text,timestamptz,text),governance.reject_dangerous_operation(text,text,text,timestamptz,text) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.activate_jit_support(text,text,text,text,timestamptz),governance.revoke_jit_support(text,text,text,timestamptz,text) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("dangerous-operation-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("dangerous-operation-manager grant commit failed")
	}
	return nil
}
