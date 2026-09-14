package artifactcap

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
)

func capabilityFixture() application.ArtifactObjectCapability {
	return application.ArtifactObjectCapability{
		Version: 1, WorkspaceID: "ws_a", RunID: "run_a", ArtifactID: "art_run_a",
		ContentSHA256: strings.Repeat("a", 64), SizeBytes: (256 << 10) + 10,
		ExpiresAt: time.Date(2026, 9, 14, 12, 5, 0, 0, time.UTC),
	}
}

func TestCodecAuthenticatesCanonicalCapability(t *testing.T) {
	codec, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.EncodeArtifactObjectCapability(capabilityFixture())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.DecodeArtifactObjectCapability(token)
	if err != nil || decoded != capabilityFixture() {
		t.Fatal(decoded, err)
	}
	parts := strings.Split(token, ".")
	parts[1] = strings.Repeat("A", len(parts[1]))
	if _, err = codec.DecodeArtifactObjectCapability(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered capability accepted")
	}
}

func TestCodecLoadsOnlyExactRegular32ByteKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.key")
	if err := os.WriteFile(path, bytes.Repeat([]byte{9}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFromFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{9}, 31), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFromFile(path); err == nil {
		t.Fatal("wrong-size key file accepted")
	}
}
