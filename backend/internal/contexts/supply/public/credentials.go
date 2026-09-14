package public

import (
	"context"
	"errors"
)

var ErrCredentialVaultUnavailable = errors.New("credential vault unavailable")

// CredentialAddress is a secret-free immutable address. It mirrors the exact
// Connection credential tuple used at runtime without exposing storage paths.
type CredentialAddress struct {
	ProviderID, ConnectionID, CredentialVersionRef string
	ConnectionRevision                             int64
}

// CredentialVault is the reviewed write/delete surface used by Connection
// authorization flows. Reads remain behind the existing runtime SecretProvider.
type CredentialVault interface {
	StoreCredential(context.Context, CredentialAddress, []byte) error
	DeleteCredential(context.Context, CredentialAddress) error
}

// CredentialResolver is intentionally separate from CredentialVault. Only
// reviewed control-plane flows such as OAuth refresh should receive it; normal
// Connection management can write/delete opaque credentials without gaining a
// read capability.
type CredentialResolver interface {
	ResolveCredential(context.Context, CredentialAddress) ([]byte, error)
}
