package connectioncredentials

import (
	"context"
	"errors"
	"time"

	connections "github.com/orz-i/mender/backend/internal/contexts/connections/public"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type Source struct {
	credentials connections.RuntimeCredentials
}

func New(credentials connections.RuntimeCredentials) *Source {
	return &Source{credentials: credentials}
}

func (s *Source) ResolveCredential(ctx context.Context, workspace, subject, connectionID, providerID string, at time.Time) (application.CredentialReference, error) {
	if s == nil || s.credentials == nil {
		return application.CredentialReference{}, application.ErrInvocationUnavailable
	}
	credential, err := s.credentials.ResolveRuntimeCredential(ctx, workspace, subject, connectionID, providerID, at)
	if err != nil {
		switch {
		case errors.Is(err, connections.ErrForbidden):
			return application.CredentialReference{}, application.ErrInvocationForbidden
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return application.CredentialReference{}, err
		default:
			return application.CredentialReference{}, application.ErrInvocationUnavailable
		}
	}
	return application.CredentialReference{ConnectionID: credential.ConnectionID, ProviderID: credential.ProviderID, CredentialVersionRef: credential.CredentialVersionRef, Revision: credential.Revision, ValidUntil: credential.ValidUntil}, nil
}

var _ application.CredentialSource = (*Source)(nil)
