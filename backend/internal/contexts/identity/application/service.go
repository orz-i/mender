package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

var (
	ErrUnauthenticated = errors.New("invalid or inactive credential")
	ErrForbidden       = errors.New("access denied")
	ErrUnavailable     = errors.New("identity unavailable")
	ErrNotFound        = errors.New("credential not found")
)

type Repository interface {
	FindCredential(context.Context, string) (domain.Credential, error)
}
type Codec interface {
	Parse(string) (id string, digest string, err error)
	EqualDigest(string, string) bool
}
type Clock interface{ Now() time.Time }
type Principal struct{ CredentialID, WorkspaceID, SubjectID string }
type Service struct {
	repository Repository
	codec      Codec
	clock      Clock
}

func NewService(repository Repository, codec Codec, clock Clock) (*Service, error) {
	if repository == nil || codec == nil || clock == nil {
		return nil, errors.New("identity requires repository, codec and clock")
	}
	return &Service{repository: repository, codec: codec, clock: clock}, nil
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	id, digest, err := s.codec.Parse(raw)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	c, err := s.repository.FindCredential(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, ErrUnavailable
	}
	if c.ID != id || !s.codec.EqualDigest(digest, c.Digest) || !c.ActiveAt(s.clock.Now()) {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{CredentialID: c.ID, WorkspaceID: c.WorkspaceID, SubjectID: c.SubjectID}, nil
}

// Re-read on every operation. There is no positive authorization cache or fallback.
// Already-authorized in-flight requests are not a promise of instantaneous revocation.
func (s *Service) Authorize(ctx context.Context, p Principal, workspace, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ValidID(p.CredentialID) || !domain.ValidID(p.SubjectID) || !domain.ValidID(workspace) || workspace != p.WorkspaceID || !domain.ValidScope(action) {
		return ErrForbidden
	}
	c, err := s.repository.FindCredential(ctx, p.CredentialID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrForbidden
		}
		return ErrUnavailable
	}
	if c.ID != p.CredentialID || c.SubjectID != p.SubjectID || c.WorkspaceID != workspace || !c.Allows(action, s.clock.Now()) {
		return ErrForbidden
	}
	return nil
}
