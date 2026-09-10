package bootstrap

import (
	"context"
	"strings"
	"testing"

	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type bootstrapSecretProvider struct{}

func (bootstrapSecretProvider) ResolveSecret(context.Context, supplyapp.SecretRequest) (supplyapp.Secret, error) {
	return supplyapp.NewSecret([]byte("not-used"))
}

func TestSupplierHTTPRuntimeRejectsUnsafeDatabaseRoleCompositionBeforeConnecting(t *testing.T) {
	base := SupplierHTTPRuntimeConfig{
		WorkerDatabaseURL:   "postgres://worker:pw@127.0.0.1:5432/mender?sslmode=disable",
		ExecutorDatabaseURL: "postgres://worker:pw@127.0.0.1:5432/mender?sslmode=disable",
		AllowedHosts:        []string{"supplier.example"},
	}
	if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), base, bootstrapSecretProvider{}); err == nil || executor != nil || closeIt != nil || !strings.Contains(err.Error(), "distinct restricted role") {
		t.Fatal(executor != nil, closeIt != nil, err)
	}
	base.ExecutorDatabaseURL = "postgres://executor:pw@127.0.0.1:5432/other?sslmode=disable"
	if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), base, bootstrapSecretProvider{}); err == nil || executor != nil || closeIt != nil || !strings.Contains(err.Error(), "same database") {
		t.Fatal(executor != nil, closeIt != nil, err)
	}
	base.ExecutorDatabaseURL = "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable"
	if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), base, nil); err == nil || executor != nil || closeIt != nil {
		t.Fatal("nil secret provider was accepted", executor != nil, closeIt != nil, err)
	}
}
