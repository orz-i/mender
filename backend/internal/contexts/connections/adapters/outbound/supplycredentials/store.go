package supplycredentials

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type Store struct{ vault supply.CredentialVault }

func New(vault supply.CredentialVault) *Store { return &Store{vault: vault} }

func address(a application.OAuthCredentialAddress) supply.CredentialAddress {
	return supply.CredentialAddress{ProviderID: a.ProviderID, ConnectionID: a.ConnectionID, CredentialVersionRef: a.CredentialVersionRef, ConnectionRevision: a.ConnectionRevision}
}

func (s *Store) Store(ctx context.Context, a application.OAuthCredentialAddress, secret []byte) error {
	if s == nil || s.vault == nil {
		return application.ErrUnavailable
	}
	if err := s.vault.StoreCredential(ctx, address(a), secret); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return application.ErrUnavailable
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, a application.OAuthCredentialAddress) error {
	if s == nil || s.vault == nil {
		return application.ErrUnavailable
	}
	if err := s.vault.DeleteCredential(ctx, address(a)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return application.ErrUnavailable
	}
	return nil
}

func (s *Store) Load(ctx context.Context, a application.OAuthCredentialAddress) ([]byte, error) {
	if s == nil || s.vault == nil {
		return nil, application.ErrUnavailable
	}
	resolver, ok := s.vault.(supply.CredentialResolver)
	if !ok {
		return nil, application.ErrUnavailable
	}
	raw, err := resolver.ResolveCredential(ctx, address(a))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, application.ErrUnavailable
	}
	if len(raw) == 0 {
		return nil, application.ErrUnavailable
	}
	return append([]byte(nil), raw...), nil
}

var _ application.OAuthCredentialStore = (*Store)(nil)
var _ application.OAuthRefreshCredentialStore = (*Store)(nil)
