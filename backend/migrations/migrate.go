// Package migrations is an explicit operator tool; API startup never applies DDL.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS
var names = []string{"0001_identity.sql", "0002_execution.sql", "0003_execution_read_indexes.sql", "0004_atomic_admission.sql", "0005_coordinated_cancellation.sql"}

func migrationBody(name string) ([]byte, error) {
	body, err := files.ReadFile(name)
	return []byte(strings.ReplaceAll(string(body), "\r\n", "\n")), err
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("migration connection failed")
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(742904001)"); err != nil {
		return errors.New("migration lock failed")
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS mender_meta; CREATE TABLE IF NOT EXISTS mender_meta.schema_migrations (name text PRIMARY KEY, sha256 text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()); REVOKE ALL ON SCHEMA mender_meta FROM PUBLIC; REVOKE ALL ON mender_meta.schema_migrations FROM PUBLIC;`); err != nil {
		return errors.New("migration metadata failed")
	}
	for _, name := range names {
		body, err := migrationBody(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		var previous string
		err = tx.QueryRow(ctx, "SELECT sha256 FROM mender_meta.schema_migrations WHERE name=$1", name).Scan(&previous)
		if err == nil {
			if previous != digest {
				return fmt.Errorf("migration checksum mismatch: %s", name)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return errors.New("migration metadata read failed")
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("migration failed: %s (database detail redacted)", name)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO mender_meta.schema_migrations(name,sha256) VALUES($1,$2)", name, digest); err != nil {
			return errors.New("migration registration failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("migration commit failed")
	}
	return nil
}

func Verify(ctx context.Context, pool *pgxpool.Pool) error {
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM mender_meta.schema_migrations").Scan(&count); err != nil || count != len(names) {
		return errors.New("required schema version unavailable")
	}
	for _, name := range names {
		body, _ := migrationBody(name)
		sum := sha256.Sum256(body)
		var digest string
		if err := pool.QueryRow(ctx, "SELECT sha256 FROM mender_meta.schema_migrations WHERE name=$1", name).Scan(&digest); err != nil || digest != hex.EncodeToString(sum[:]) {
			return errors.New("required schema checksum unavailable")
		}
	}
	return nil
}

// GrantRuntime only grants to an existing, non-privileged role. It creates no role/password.
func GrantRuntime(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid runtime role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb FROM pg_roles WHERE rolname=$1", role).Scan(&elevated); err != nil || elevated {
		return errors.New("runtime role must exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA identity,execution,mender_meta TO " + id,
		"GRANT SELECT ON identity.workspaces,identity.service_accounts,identity.api_keys,mender_meta.schema_migrations,execution.runs TO " + id,
		"GRANT UPDATE (state,version,updated_at) ON execution.runs TO " + id,
		"GRANT SELECT,INSERT ON execution.run_events TO " + id,
		"GRANT SELECT (workspace_id,run_id) ON execution.run_admissions TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("runtime grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("runtime grant commit failed")
	}
	return nil
}
