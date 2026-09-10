package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

// RunOperator is local, privileged administration, not a public registration endpoint.
// Only issue-key intentionally prints a generated secret, once, after durable insertion.
func RunOperator(ctx context.Context, args []string, getenv func(string) string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator requires migrate, grant-runtime, grant-cancellation, issue-key or revoke-key")
	}
	command := args[0]
	if command != "migrate" && command != "grant-runtime" && command != "grant-cancellation" && command != "issue-key" && command != "revoke-key" {
		return errors.New("unknown operator command")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	workspace := flags.String("workspace", "", "Workspace ID")
	subject := flags.String("subject", "", "service account ID")
	scopes := flags.String("scopes", "run:read", "comma-separated run:read,run:cancel")
	ttl := flags.Duration("ttl", 24*time.Hour, "credential lifetime (1 minute to 90 days)")
	role := flags.String("role", "", "existing restricted PostgreSQL login role")
	id := flags.String("id", "", "key ID to revoke (never the raw secret)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	var record domain.Credential
	var raw string
	if command == "issue-key" {
		if *ttl < time.Minute || *ttl > 90*24*time.Hour {
			return errors.New("invalid credential lifetime")
		}
		token, keyID, digest, err := (keycodec.Codec{}).Generate()
		if err != nil {
			return errors.New("credential randomness unavailable")
		}
		raw = token
		at := time.Now().UTC().Truncate(time.Microsecond)
		record = domain.Credential{ID: keyID, WorkspaceID: *workspace, SubjectID: *subject, Digest: digest, Scopes: strings.Split(*scopes, ","), CreatedAt: at, ExpiresAt: at.Add(*ttl)}
		if err = record.Validate(); err != nil {
			return err
		}
	}
	dsn := getenv("MENDER_ADMIN_DATABASE_URL")
	if dsn == "" {
		return errors.New("MENDER_ADMIN_DATABASE_URL is required; API runtime credentials are not used for administration")
	}
	pool, err := database.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if command == "migrate" {
		if err = migrations.Apply(ctx, pool); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Mender schema migrations applied.\n")
		return err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return err
	}
	switch command {
	case "grant-cancellation":
		if err = migrations.GrantCancellation(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted cancellation grants applied; API startup verifies the target role before enabling the feature.\n")
		return err
	case "grant-runtime":
		if err = migrations.GrantRuntime(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted runtime grants applied.\n")
		return err
	case "revoke-key":
		if err = identitypg.New(pool).Revoke(ctx, *id); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Credential revoked.\n")
		return err
	case "issue-key":
		if err = identitypg.New(pool).Provision(ctx, record); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(struct {
			KeyID     string    `json:"key_id"`
			APIKey    string    `json:"api_key"`
			ExpiresAt time.Time `json:"expires_at"`
		}{record.ID, raw, record.ExpiresAt})
	}
	return errors.New("operator command not implemented")
}
