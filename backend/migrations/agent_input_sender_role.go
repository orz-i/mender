package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GrantAgentInputSender(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid agent-input-sender role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("agent-input-sender role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("agent-input-sender grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON execution.runs,execution.run_attempts,execution.agent_input_requests TO " + id,
		"GRANT UPDATE (state,answer_sha256,submission_id,sending_at,submitted_at,updated_at) ON execution.agent_input_requests TO " + id,
		"GRANT UPDATE (state,version,updated_at) ON execution.runs TO " + id,
		"GRANT INSERT ON execution.run_events TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("agent-input-sender grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("agent-input-sender grant commit failed")
	}
	return nil
}
