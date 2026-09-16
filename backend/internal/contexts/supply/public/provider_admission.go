package public

import (
	"context"
	"errors"
)

var ErrProviderQuarantined = errors.New("provider quarantined")
var ErrProviderAdmissionUnavailable = errors.New("provider admission unavailable")

type ProviderAdmissionGate interface {
	EnsureProviderAvailable(context.Context, string) error
}
