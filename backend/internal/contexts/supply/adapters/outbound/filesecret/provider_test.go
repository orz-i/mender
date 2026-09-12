package filesecret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

func request() supplyapp.SecretRequest {
	return supplyapp.SecretRequest{ProviderID: "provider_a", ConnectionID: "conn_a", CredentialVersionRef: "credv_a", ConnectionRevision: 3}
}

func TestProviderResolvesOnlyExactBoundSecret(t *testing.T) {
	root := t.TempDir()
	provider, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	name, err := FileName(request())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(name, "provider_a") || strings.Contains(name, "conn_a") || filepath.Base(name) != name {
		t.Fatal("secret filename leaked routing data or became a path", name)
	}
	if err = os.WriteFile(filepath.Join(root, name), []byte("reviewed-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, err := provider.ResolveSecret(context.Background(), request())
	if err != nil || string(secret.Bytes()) != "reviewed-secret" || secret.String() != "[REDACTED]" {
		t.Fatal("mounted secret did not resolve safely", secret, err)
	}
	changed := request()
	changed.ConnectionRevision++
	if _, err = provider.ResolveSecret(context.Background(), changed); !errors.Is(err, supplyapp.ErrSecretUnavailable) {
		t.Fatal("different connection revision reused mounted secret", err)
	}
}

func TestProviderRejectsEscapesInvalidFilesAndOversizeSecrets(t *testing.T) {
	root := t.TempDir()
	provider, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	bad := request()
	bad.CredentialVersionRef = "../escape"
	if _, err = FileName(bad); !errors.Is(err, supplyapp.ErrSecretUnavailable) {
		t.Fatal("invalid credential reference became a mount path", err)
	}
	name, _ := FileName(request())
	if err = os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.ResolveSecret(context.Background(), request()); !errors.Is(err, supplyapp.ErrSecretUnavailable) {
		t.Fatal("directory accepted as secret file", err)
	}
	if err = os.Remove(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, name), make([]byte, maxSecretBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.ResolveSecret(context.Background(), request()); !errors.Is(err, supplyapp.ErrSecretUnavailable) {
		t.Fatal("oversize secret accepted", err)
	}
}

func TestProviderAllowsContainedProjectionSymlinkButRejectsEscape(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name, _ := FileName(request())
	target := filepath.Join(dataDir, "current")
	if err := os.WriteFile(target, []byte("projected-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("data", "current"), filepath.Join(root, name)); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
	provider, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := provider.ResolveSecret(context.Background(), request())
	if err != nil || string(secret.Bytes()) != "projected-secret" {
		t.Fatal("contained projected secret was rejected", err)
	}
	if err = os.Remove(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(outside, []byte("must-not-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, name)); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
	if _, err = provider.ResolveSecret(context.Background(), request()); !errors.Is(err, supplyapp.ErrSecretUnavailable) {
		t.Fatal("symlink escape was accepted", err)
	}
}

func TestProviderPropagatesCallerCancellationBeforeFileAccess(t *testing.T) {
	provider, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = provider.ResolveSecret(ctx, request()); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation was hidden", err)
	}
}
