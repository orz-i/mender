package bootstrap

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactObjectReadConfigIsExplicitAndBounded(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "artifact-object.key")
	if err := os.WriteFile(keyFile, bytes.Repeat([]byte{5}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	cursorKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                     "true",
		"MENDER_RUN_READ_API_ENABLED":                "true",
		"MENDER_DATABASE_URL":                        "postgres://runtime:not-opened@127.0.0.1/mender",
		"MENDER_CURSOR_SIGNING_KEY":                  cursorKey,
		"MENDER_ARTIFACT_OBJECT_READ_ENABLED":        "true",
		"MENDER_ARTIFACT_OBJECT_READER_DATABASE_URL": "postgres://object_reader:not-opened@127.0.0.1/mender",
		"MENDER_ARTIFACT_OBJECT_ROOT":                t.TempDir(),
		"MENDER_ARTIFACT_OBJECT_SIGNING_KEY_FILE":    keyFile,
		"MENDER_ARTIFACT_OBJECT_CAPABILITY_TTL":      "90s",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return valid[key] })
	if err != nil || !cfg.ArtifactObjectReadEnabled || cfg.ArtifactObjectCapabilityTTL != 90*time.Second {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_RUN_READ_API_ENABLED": "false"},
		{"MENDER_ARTIFACT_OBJECT_READ_ENABLED": "yes"},
		{"MENDER_ARTIFACT_OBJECT_READER_DATABASE_URL": ""},
		{"MENDER_ARTIFACT_OBJECT_ROOT": ""},
		{"MENDER_ARTIFACT_OBJECT_SIGNING_KEY_FILE": ""},
		{"MENDER_ARTIFACT_OBJECT_CAPABILITY_TTL": "10s"},
		{"MENDER_ARTIFACT_OBJECT_CAPABILITY_TTL": "6m"},
	} {
		_, err = LoadAPIConfig(func(key string) string {
			if value, ok := change[key]; ok {
				return value
			}
			return valid[key]
		})
		if err == nil {
			t.Fatal("unsafe Artifact object read config accepted", change)
		}
	}
	badKey := filepath.Join(t.TempDir(), "short.key")
	if err = os.WriteFile(badKey, bytes.Repeat([]byte{1}, 31), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadAPIConfig(func(key string) string {
		if key == "MENDER_ARTIFACT_OBJECT_SIGNING_KEY_FILE" {
			return badKey
		}
		return valid[key]
	})
	if err == nil || !strings.Contains(err.Error(), "signing key") {
		t.Fatal("invalid Artifact signing key file accepted", err)
	}
}
