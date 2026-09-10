package public

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden   = errors.New("connection access denied")
	ErrUnavailable = errors.New("connections unavailable")
)

type Access struct {
	ConnectionID, ProviderID string
	Revision                 int64
	ValidUntil               time.Time
}

type Connections interface {
	ResolveAccess(context.Context, string, string, string, string, time.Time) (Access, error)
}
