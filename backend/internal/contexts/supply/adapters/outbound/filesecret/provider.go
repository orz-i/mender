package filesecret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supplypublic "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

const maxSecretBytes = 16 << 10

type Provider struct{ root string }

// New constructs a SecretProvider over an operator-managed mounted directory.
// The root is resolved once so projected-secret symlinks may be used, but each
// resolved secret file must remain inside the resolved root.
func New(root string) (*Provider, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("secret root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.New("secret root is invalid")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, errors.New("secret root is unavailable")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, errors.New("secret root must be a directory")
	}
	return &Provider{root: resolved}, nil
}

func addressRequest(address supplypublic.CredentialAddress) supplyapp.SecretRequest {
	return supplyapp.SecretRequest{
		ProviderID:           address.ProviderID,
		ConnectionID:         address.ConnectionID,
		CredentialVersionRef: address.CredentialVersionRef,
		ConnectionRevision:   address.ConnectionRevision,
	}
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func validRequest(request supplyapp.SecretRequest) bool {
	return validID(request.ProviderID) && validID(request.ConnectionID) && validID(request.CredentialVersionRef) && request.ConnectionRevision > 0
}

// FileName returns the deterministic mount filename for an exact reviewed
// credential request. No caller-controlled field becomes a path component.
func FileName(request supplyapp.SecretRequest) (string, error) {
	if !validRequest(request) {
		return "", supplyapp.ErrSecretUnavailable
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("mender-secret-v1\x00%s\x00%s\x00%s\x00%s", request.ProviderID, request.ConnectionID, request.CredentialVersionRef, strconv.FormatInt(request.ConnectionRevision, 10))))
	return hex.EncodeToString(digest[:]) + ".secret", nil
}

func contained(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p *Provider) ResolveSecret(ctx context.Context, request supplyapp.SecretRequest) (supplyapp.Secret, error) {
	if err := ctx.Err(); err != nil {
		return supplyapp.Secret{}, err
	}
	if p == nil || p.root == "" {
		return supplyapp.Secret{}, supplyapp.ErrSecretUnavailable
	}
	name, err := FileName(request)
	if err != nil {
		return supplyapp.Secret{}, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(p.root, name))
	if err != nil || !contained(p.root, resolved) {
		return supplyapp.Secret{}, supplyapp.ErrSecretUnavailable
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxSecretBytes {
		return supplyapp.Secret{}, supplyapp.ErrSecretUnavailable
	}
	file, err := os.Open(resolved)
	if err != nil {
		return supplyapp.Secret{}, supplyapp.ErrSecretUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	if err != nil || len(data) < 1 || len(data) > maxSecretBytes {
		return supplyapp.Secret{}, supplyapp.ErrSecretUnavailable
	}
	secret, err := supplyapp.NewSecret(data)
	if err != nil {
		return supplyapp.Secret{}, supplyapp.ErrSecretUnavailable
	}
	return secret, nil
}

func (p *Provider) StoreCredential(ctx context.Context, address supplypublic.CredentialAddress, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.root == "" || len(raw) < 1 || len(raw) > maxSecretBytes {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	name, err := FileName(addressRequest(address))
	if err != nil {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	path := filepath.Join(p.root, name)
	if !contained(p.root, path) {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err = file.Write(raw); err != nil || file.Sync() != nil || file.Close() != nil {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	complete = true
	return nil
}

func (p *Provider) DeleteCredential(ctx context.Context, address supplypublic.CredentialAddress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.root == "" {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	name, err := FileName(addressRequest(address))
	if err != nil {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	path := filepath.Join(p.root, name)
	if !contained(p.root, path) {
		return supplypublic.ErrCredentialVaultUnavailable
	}
	err = os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return supplypublic.ErrCredentialVaultUnavailable
}

var _ supplyapp.SecretProvider = (*Provider)(nil)
var _ supplypublic.CredentialVault = (*Provider)(nil)
