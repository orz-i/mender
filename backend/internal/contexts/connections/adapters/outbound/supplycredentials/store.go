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

var _ application.OAuthCredentialStore = (*Store)(nil)
