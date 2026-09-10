package identity_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type clock struct{ at time.Time }

func (c clock) Now() time.Time { return c.at }

type store struct {
	record domain.Credential
	err    error
	reads  int
}

func (s *store) FindCredential(ctx context.Context, id string) (domain.Credential, error) {
	s.reads++
	if err := ctx.Err(); err != nil {
		return domain.Credential{}, err
	}
	if s.err != nil {
		return domain.Credential{}, s.err
	}
	if id != s.record.ID {
		return domain.Credential{}, application.ErrNotFound
	}
	return s.record, nil
}

var now = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (*application.Service, *store, string) {
	t.Helper()
	raw, id, digest, err := (keycodec.Codec{}).Generate()
	if err != nil {
		t.Fatal(err)
	}
	repo := &store{record: domain.Credential{ID: id, WorkspaceID: "ws_a", SubjectID: "sa_a", Digest: digest, Scopes: []string{"run:read", "run:cancel"}, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}
	s, err := application.NewService(repo, keycodec.Codec{}, clock{now})
	if err != nil {
		t.Fatal(err)
	}
	return s, repo, raw
}

func TestMachineCredentialRoundTripAndMalformedTokens(t *testing.T) {
	s, repo, raw := fixture(t)
	p, err := s.Authenticate(context.Background(), raw)
	if err != nil || p.SubjectID != "sa_a" || p.WorkspaceID != "ws_a" || p.CredentialID != repo.record.ID {
		t.Fatal(p, err)
	}
	if strings.Contains(repo.record.Digest, raw) {
		t.Fatal("secret stored as verifier")
	}
	if err = s.Authorize(context.Background(), p, "ws_a", "run:cancel"); err != nil {
		t.Fatal(err)
	}
	before := repo.reads
	for _, bad := range []string{"", raw + " ", strings.ToUpper(raw), "Bearer " + raw, raw[:44] + "/" + raw[45:], strings.Repeat("x", 10000)} {
		if _, err = s.Authenticate(context.Background(), bad); !errors.Is(err, application.ErrUnauthenticated) {
			t.Fatal("malformed accepted", err)
		}
	}
	if repo.reads != before {
		t.Fatal("malformed keys reached database")
	}
	other, _, _, _ := (keycodec.Codec{}).Generate()
	wrong := raw[:45] + other[45:]
	if _, err = s.Authenticate(context.Background(), wrong); !errors.Is(err, application.ErrUnauthenticated) {
		t.Fatal("wrong secret accepted", err)
	}
}

func TestRevocationExpiryMembershipAndScopeAreRechecked(t *testing.T) {
	for name, mutate := range map[string]func(*domain.Credential){
		"revoked": func(c *domain.Credential) { c.Revoked = true }, "workspace disabled": func(c *domain.Credential) { c.WorkspaceDisabled = true },
		"subject disabled": func(c *domain.Credential) { c.SubjectDisabled = true }, "expiry boundary": func(c *domain.Credential) { c.ExpiresAt = now },
		"not yet issued": func(c *domain.Credential) { c.CreatedAt = now.Add(time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			s, r, raw := fixture(t)
			p, err := s.Authenticate(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&r.record)
			if err = s.Authorize(context.Background(), p, "ws_a", "run:read"); !errors.Is(err, application.ErrForbidden) {
				t.Fatal(err)
			}
			if _, err = s.Authenticate(context.Background(), raw); !errors.Is(err, application.ErrUnauthenticated) {
				t.Fatal(err)
			}
		})
	}
	s, r, raw := fixture(t)
	p, _ := s.Authenticate(context.Background(), raw)
	r.record.Scopes = []string{"run:read"}
	if err := s.Authorize(context.Background(), p, "ws_a", "run:cancel"); !errors.Is(err, application.ErrForbidden) {
		t.Fatal(err)
	}
	before := r.reads
	if err := s.Authorize(context.Background(), p, "ws_b", "run:read"); !errors.Is(err, application.ErrForbidden) {
		t.Fatal(err)
	}
	if err := s.Authorize(context.Background(), p, "ws_a", "workspace:admin"); !errors.Is(err, application.ErrForbidden) {
		t.Fatal(err)
	}
	if r.reads != before {
		t.Fatal("invalid target reached storage")
	}
}

func TestMissingKeyDatabaseFailureAndCanceledContextFailClosed(t *testing.T) {
	s, r, raw := fixture(t)
	p, _ := s.Authenticate(context.Background(), raw)
	for _, dbErr := range []error{application.ErrNotFound, errors.New("private database password must not escape")} {
		r.err = dbErr
		_, err := s.Authenticate(context.Background(), raw)
		if errors.Is(dbErr, application.ErrNotFound) {
			if !errors.Is(err, application.ErrUnauthenticated) {
				t.Fatal(err)
			}
		} else if !errors.Is(err, application.ErrUnavailable) {
			t.Fatal(err)
		}
		if err == dbErr {
			t.Fatal("raw dependency failure escaped")
		}
		if err = s.Authorize(context.Background(), p, "ws_a", "run:read"); err == nil {
			t.Fatal("authorization fell back")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := r.reads
	if _, err := s.Authenticate(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.reads != before {
		t.Fatal("canceled auth reached storage")
	}
	if _, err := application.NewService(nil, keycodec.Codec{}, clock{now}); err == nil {
		t.Fatal("nil repository accepted")
	}
}

func TestCredentialRecordValidationAndDetachedScopeSemantics(t *testing.T) {
	_, r, _ := fixture(t)
	for _, mutate := range []func(*domain.Credential){func(c *domain.Credential) { c.WorkspaceID = "../ws" }, func(c *domain.Credential) { c.Digest = "z" + c.Digest[1:] }, func(c *domain.Credential) { c.Scopes = []string{"admin"} }, func(c *domain.Credential) { c.Scopes = nil }, func(c *domain.Credential) { c.ExpiresAt = c.CreatedAt }} {
		c := r.record
		mutate(&c)
		if c.Validate() == nil || c.ActiveAt(now) {
			t.Fatal("invalid credential active")
		}
	}
}
