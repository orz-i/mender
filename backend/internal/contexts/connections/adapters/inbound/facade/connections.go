package facade

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
	connections "github.com/orz-i/mender/backend/internal/contexts/connections/public"
)

type Connections struct{ service *application.Service }

func New(service *application.Service) *Connections { return &Connections{service: service} }

func (f *Connections) ResolveAccess(ctx context.Context, workspace, subject, connectionID, providerID string, at time.Time) (connections.Access, error) {
	a, err := f.service.Resolve(ctx, workspace, subject, connectionID, providerID, at)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrForbidden):
			return connections.Access{}, connections.ErrForbidden
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return connections.Access{}, err
		default:
			return connections.Access{}, connections.ErrUnavailable
		}
	}
	return connections.Access{ConnectionID: a.ConnectionID, ProviderID: a.ProviderID, Revision: a.Revision, ValidUntil: a.ValidUntil()}, nil
}

var _ connections.Connections = (*Connections)(nil)
