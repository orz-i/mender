package application

import (
	"context"
	"errors"
)

type ProviderAdmissionRepository interface {
	ProviderAcceptsNewWork(context.Context, string) error
}

type ProviderAdmission struct{ repository ProviderAdmissionRepository }

func NewProviderAdmission(repository ProviderAdmissionRepository) (*ProviderAdmission, error) {
	if repository == nil {
		return nil, ErrInvocationUnavailable
	}
	return &ProviderAdmission{repository: repository}, nil
}

func validProviderID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func (s *ProviderAdmission) EnsureAvailable(ctx context.Context, provider string) error {
	if !validProviderID(provider) {
		return ErrInvocationUnavailable
	}
	err := s.repository.ProviderAcceptsNewWork(ctx, provider)
	if err == nil || errors.Is(err, ErrProviderQuarantined) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrInvocationUnavailable
}
