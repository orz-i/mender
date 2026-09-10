package facade

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
	connections "github.com/orz-i/mender/backend/internal/contexts/connections/public"
)

type RuntimeCredentials struct{ service *application.RuntimeService }

func NewRuntimeCredentials(service *application.RuntimeService) *RuntimeCredentials {
	return &RuntimeCredentials{service: service}
}

func (f *RuntimeCredentials) ResolveRuntimeCredential(ctx context.Context, workspace, subject, connectionID, providerID string, at time.Time) (connections.RuntimeCredential, error) {
	value, err := f.service.Resolve(ctx, workspace, subject, connectionID, providerID, at)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrForbidden):
			return connections.RuntimeCredential{}, connections.ErrForbidden
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return connections.RuntimeCredential{}, err
		default:
			return connections.RuntimeCredential{}, connections.ErrUnavailable
		}
	}
	return connections.RuntimeCredential{ConnectionID: value.ConnectionID, ProviderID: value.ProviderID, CredentialVersionRef: value.CredentialVersionRef, Revision: value.Revision, ValidUntil: value.ValidUntil()}, nil
}

var _ connections.RuntimeCredentials = (*RuntimeCredentials)(nil)
