//go:build integration

package migrations

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplyThroughForIntegration exists only in integration-tag builds. Production
// operators can only call Apply, which always targets the complete migration set.
func ApplyThroughForIntegration(ctx context.Context, pool *pgxpool.Pool, last string) error {
	for i, name := range names {
		if name == last {
			return applySelected(ctx, pool, names[:i+1])
		}
	}
	return errors.New("unknown integration migration boundary")
}
