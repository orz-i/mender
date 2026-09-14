package filesecret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

const maxSecretBytes = 256

type Provider struct {
	root string
	keys map[string]map[string]struct{}
}

func validID(value string) bool {
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

func New(root string, reviewed map[string][]string) (*Provider, error) {
	if strings.TrimSpace(root) == "" || len(reviewed) == 0 || len(reviewed) > 256 {
		return nil, application.ErrUnavailable
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, application.ErrUnavailable
	}
	keys := make(map[string]map[string]struct{}, len(reviewed))
	for provider, values := range reviewed {
		if !validID(provider) || len(values) < 1 || len(values) > 2 {
			return nil, application.ErrUnavailable
		}
		seen := map[string]struct{}{}
		for _, keyID := range values {
			if !validID(keyID) {
				return nil, application.ErrUnavailable
			}
			if _, duplicate := seen[keyID]; duplicate {
				return nil, application.ErrUnavailable
			}
			seen[keyID] = struct{}{}
		}
		keys[provider] = seen
	}
	return &Provider{root: resolved, keys: keys}, nil
}

func FileName(provider, keyID string) (string, error) {
	if !validID(provider) || !validID(keyID) {
		return "", application.ErrUnavailable
	}
	digest := sha256.Sum256([]byte("mender-callback-secret-v1\x00" + provider + "\x00" + keyID))
	return hex.EncodeToString(digest[:]) + ".secret", nil
}

func contained(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p *Provider) ResolveCallbackSecret(ctx context.Context, provider, keyID string) (application.Secret, error) {
	if err := ctx.Err(); err != nil {
		return application.Secret{}, err
	}
	if p == nil || p.root == "" {
		return application.Secret{}, application.ErrUnavailable
	}
	providerKeys, ok := p.keys[provider]
	if !ok {
		return application.Secret{}, application.ErrUnavailable
	}
	if _, ok = providerKeys[keyID]; !ok {
		return application.Secret{}, application.ErrUnavailable
	}
	name, err := FileName(provider, keyID)
	if err != nil {
		return application.Secret{}, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(p.root, name))
	if err != nil || !contained(p.root, resolved) {
		return application.Secret{}, application.ErrUnavailable
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > maxSecretBytes {
		return application.Secret{}, application.ErrUnavailable
	}
	file, err := os.Open(resolved)
	if err != nil {
		return application.Secret{}, application.ErrUnavailable
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	if err != nil || len(raw) < 32 || len(raw) > maxSecretBytes {
		return application.Secret{}, application.ErrUnavailable
	}
	secret, err := application.NewSecret(raw)
	if err != nil {
		return application.Secret{}, application.ErrUnavailable
	}
	return secret, nil
}

var _ application.SecretSource = (*Provider)(nil)
