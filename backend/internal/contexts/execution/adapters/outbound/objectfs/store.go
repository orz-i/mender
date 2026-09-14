package objectfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var ErrObjectStoreUnavailable = errors.New("artifact filesystem object store unavailable")

type Store struct{ root string }

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrObjectStoreUnavailable
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrObjectStoreUnavailable
	}
	if info, statErr := os.Lstat(abs); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrObjectStoreUnavailable
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, ErrObjectStoreUnavailable
	}
	if err = os.MkdirAll(abs, 0o700); err != nil {
		return nil, ErrObjectStoreUnavailable
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrObjectStoreUnavailable
	}
	return &Store{root: filepath.Clean(abs)}, nil
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func identity(candidate application.ArtifactObjectCandidate) string {
	return digest([]byte(string(candidate.WorkspaceID) + "\x00" + string(candidate.RunID) + "\x00" + candidate.ArtifactID))
}

func (s *Store) pathFor(key string) (string, error) {
	if s == nil || !domain.ValidArtifactObjectKey(key) {
		return "", ErrObjectStoreUnavailable
	}
	target := filepath.Clean(filepath.Join(s.root, filepath.FromSlash(key)))
	rel, err := filepath.Rel(s.root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrObjectStoreUnavailable
	}
	return target, nil
}

func ensureDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrObjectStoreUnavailable
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrObjectStoreUnavailable
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return ErrObjectStoreUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrObjectStoreUnavailable
	}
	return nil
}

func (s *Store) ensureParents(key string) error {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return ErrObjectStoreUnavailable
	}
	first := filepath.Join(s.root, parts[0])
	if err := ensureDirectory(first); err != nil {
		return err
	}
	return ensureDirectory(filepath.Join(first, parts[1]))
}

func verifyExisting(path string, content []byte, expectedSHA string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != int64(len(content)) || info.Size() > 1<<20 {
		return ErrObjectStoreUnavailable
	}
	existing, err := os.ReadFile(path)
	if err != nil || digest(existing) != expectedSHA || !bytes.Equal(existing, content) {
		return ErrObjectStoreUnavailable
	}
	return nil
}

func (s *Store) PutArtifactObject(ctx context.Context, candidate application.ArtifactObjectCandidate) (application.StoredArtifactObject, error) {
	if err := ctx.Err(); err != nil {
		return application.StoredArtifactObject{}, err
	}
	if !candidate.Valid() {
		return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
	}
	content := []byte(candidate.ContentJSON)
	contentSHA := digest(content)
	key := "objects/" + identity(candidate) + "/" + contentSHA + ".json"
	path, err := s.pathFor(key)
	if err != nil || s.ensureParents(key) != nil {
		return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
	}
	if _, err = os.Lstat(path); err == nil {
		if verifyExisting(path, content, contentSHA) != nil {
			return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
		}
		return application.StoredArtifactObject{ObjectKey: key, ContentSHA256: contentSHA, SizeBytes: int64(len(content))}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mender-artifact-*")
	if err != nil {
		return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	defer cleanup()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return application.StoredArtifactObject{}, ctx.Err()
		}
		return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
	}
	if err = os.Rename(tmpName, path); err != nil {
		if verifyExisting(path, content, contentSHA) != nil {
			return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
		}
	}
	if verifyExisting(path, content, contentSHA) != nil {
		return application.StoredArtifactObject{}, ErrObjectStoreUnavailable
	}
	return application.StoredArtifactObject{ObjectKey: key, ContentSHA256: contentSHA, SizeBytes: int64(len(content))}, nil
}

func (s *Store) ReadArtifactObject(ctx context.Context, key, expectedSHA string, expectedSize int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !domain.ValidArtifactObjectKey(key) || !domain.ValidSHA256(expectedSHA) || expectedSize <= application.ArtifactObjectThresholdBytes || expectedSize > 1<<20 {
		return nil, ErrObjectStoreUnavailable
	}
	path, err := s.pathFor(key)
	if err != nil {
		return nil, ErrObjectStoreUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != expectedSize {
		return nil, ErrObjectStoreUnavailable
	}
	content, err := os.ReadFile(path)
	if err != nil || int64(len(content)) != expectedSize || digest(content) != expectedSHA {
		return nil, ErrObjectStoreUnavailable
	}
	return content, nil
}

func (s *Store) DeleteArtifactObject(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.pathFor(key)
	if err != nil {
		return ErrObjectStoreUnavailable
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ErrObjectStoreUnavailable
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrObjectStoreUnavailable
	}
	return nil
}
