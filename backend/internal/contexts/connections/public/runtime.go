package public

import (
	"context"
	"time"
)

type RuntimeCredential struct {
	ConnectionID, ProviderID, CredentialVersionRef string
	Revision                                       int64
	ValidUntil                                     time.Time
}

type RuntimeCredentials interface {
	ResolveRuntimeCredential(context.Context, string, string, string, string, time.Time) (RuntimeCredential, error)
}
