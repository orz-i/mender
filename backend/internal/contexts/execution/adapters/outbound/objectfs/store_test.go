package objectfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
)

func fsCandidate() application.ArtifactObjectCandidate {
	content := `{"payload":"` + strings.Repeat("x", (256<<10)+64) + `"}`
	return application.ArtifactObjectCandidate{WorkspaceID: "ws_a", RunID: "run_a", ArtifactID: "art_run_a", MediaType: "application/json", ContentJSON: content, CreatedAt: time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)}
}

func TestFilesystemStoreUsesRootContainedAtomicIdempotentWrites(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	candidate := fsCandidate()
	first, err := store.PutArtifactObject(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutArtifactObject(context.Background(), candidate)
	if err != nil || first != second {
		t.Fatal(first, second, err)
	}
	path, err := store.pathFor(first.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != candidate.ContentJSON {
		t.Fatal(err, len(body))
	}
	if rel, err := filepath.Rel(root, path); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatal("object escaped root", rel, err)
	}
	if err = os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutArtifactObject(context.Background(), candidate); err == nil {
		t.Fatal("tampered object was silently overwritten")
	}
}

func TestFilesystemStoreRejectsSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable on this host", err)
	}
	if _, err := New(link); err == nil {
		t.Fatal("symlink root accepted")
	}
}
