package filesecret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

func TestProviderAllowsOnlyReviewedProviderKeyPairsAndRotationOverlap(t *testing.T) {
	root := t.TempDir()
	provider, err := New(root, map[string][]string{"provider_a": {"key_old", "key_new"}})
	if err != nil {
		t.Fatal(err)
	}
	oldName, err := FileName("provider_a", "key_old")
	if err != nil {
		t.Fatal(err)
	}
	newName, err := FileName("provider_a", "key_new")
	if err != nil {
		t.Fatal(err)
	}
	old := []byte("old-secret-0123456789abcdef0123456789abcdef")
	newer := []byte("new-secret-0123456789abcdef0123456789abcdef")
	if err = os.WriteFile(filepath.Join(root, oldName), old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, newName), newer, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, keyID := range []string{"key_old", "key_new"} {
		secret, resolveErr := provider.ResolveCallbackSecret(context.Background(), "provider_a", keyID)
		if resolveErr != nil || secret.String() != "[REDACTED]" {
			t.Fatal(keyID, secret, resolveErr)
		}
	}
	if _, err = provider.ResolveCallbackSecret(context.Background(), "provider_a", "key_unknown"); !errors.Is(err, application.ErrUnavailable) {
		t.Fatal("unreviewed callback key was accepted", err)
	}
	if _, err = provider.ResolveCallbackSecret(context.Background(), "provider_b", "key_old"); !errors.Is(err, application.ErrUnavailable) {
		t.Fatal("other provider reused callback key", err)
	}
}

func TestProviderRejectsInvalidReviewSetsAndSecretEscapes(t *testing.T) {
	root := t.TempDir()
	for _, reviewed := range []map[string][]string{
		nil,
		{"provider_a": nil},
		{"bad provider": {"key_a"}},
		{"provider_a": {"key_a", "key_a"}},
		{"provider_a": {"key_a", "key_b", "key_c"}},
	} {
		if provider, err := New(root, reviewed); err == nil || provider != nil {
			t.Fatal("invalid reviewed key set accepted", reviewed, err)
		}
	}
	provider, err := New(root, map[string][]string{"provider_a": {"key_a"}})
	if err != nil {
		t.Fatal(err)
	}
	name, err := FileName("provider_a", "key_a")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.secret")
	if err = os.WriteFile(outside, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, name)); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
	if _, err = provider.ResolveCallbackSecret(context.Background(), "provider_a", "key_a"); !errors.Is(err, application.ErrUnavailable) {
		t.Fatal("callback secret symlink escaped mounted root", err)
	}
}
