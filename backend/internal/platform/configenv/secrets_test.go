package configenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMountedSecretsSnapshotAndNoAuthorityExpansion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(p, []byte("postgres://example-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MENDER_DATABASE_URL_FILE": p, "MENDER_RUN_API_ENABLED_FILE": p}
	get := func(k string) string { return env[k] }
	resolved, err := Resolve(get)
	if err != nil || resolved("MENDER_DATABASE_URL") != "postgres://example-secret" {
		t.Fatal("file not resolved", err)
	}
	if resolved("MENDER_RUN_API_ENABLED") != "" || get("MENDER_DATABASE_URL") != "" {
		t.Fatal("authority or ambient env mutated")
	}
	again, err := Resolve(resolved)
	if err != nil || again("MENDER_DATABASE_URL") != "postgres://example-secret" {
		t.Fatal("nested outer composition must not reinterpret a resolved file", err)
	}
	if err := os.WriteFile(p, []byte("rotated"), 0600); err != nil {
		t.Fatal(err)
	}
	if resolved("MENDER_DATABASE_URL") != "postgres://example-secret" {
		t.Fatal("configuration was not snapshotted")
	}
}

func TestInvalidAndConflictingSecretsFailRedacted(t *testing.T) {
	for _, body := range []string{"", "a\nb", "a\x00b", strings.Repeat("x", 16385)} {
		p := filepath.Join(t.TempDir(), "secret-contents-must-not-leak")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Resolve(func(k string) string {
			if k == "MENDER_DATABASE_URL_FILE" {
				return p
			}
			return ""
		})
		if err == nil || strings.Contains(err.Error(), p) {
			t.Fatal("invalid secret accepted or path leaked", err)
		}
	}
	for _, p := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := Resolve(func(k string) string {
			if k == "MENDER_DATABASE_URL_FILE" {
				return p
			}
			return ""
		}); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
	if _, err := Resolve(func(k string) string {
		if k == "MENDER_DATABASE_URL" {
			return "raw-secret"
		}
		if k == "MENDER_DATABASE_URL_FILE" {
			return "file-secret"
		}
		return ""
	}); err == nil || strings.Contains(err.Error(), "raw-secret") {
		t.Fatal("conflict not rejected", err)
	}
}
