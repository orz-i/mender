package paymentsecret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

const maxSecretBytes = 256

type Provider struct {
	root string
	keys map[string]map[string]struct{}
}

func validSegment(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == ':') {
			return false
		}
	}
	return true
}

func New(root string, reviewed map[string][]string) (*Provider, error) {
	if strings.TrimSpace(root) == "" || len(reviewed) < 1 || len(reviewed) > 256 {
		return nil, application.ErrPaymentCallbackUnavailable
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, application.ErrPaymentCallbackUnavailable
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, application.ErrPaymentCallbackUnavailable
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, application.ErrPaymentCallbackUnavailable
	}
	keys := make(map[string]map[string]struct{}, len(reviewed))
	for providerAccount, values := range reviewed {
		parts := strings.Split(providerAccount, "/")
		if len(parts) != 2 || !validSegment(parts[0]) || !validSegment(parts[1]) || len(values) < 1 || len(values) > 2 {
			return nil, application.ErrPaymentCallbackUnavailable
		}
		seen := map[string]struct{}{}
		for _, key := range values {
			if !validSegment(key) {
				return nil, application.ErrPaymentCallbackUnavailable
			}
			if _, exists := seen[key]; exists {
				return nil, application.ErrPaymentCallbackUnavailable
			}
			seen[key] = struct{}{}
		}
		keys[providerAccount] = seen
	}
	return &Provider{root: resolved, keys: keys}, nil
}

func FileName(provider, account, keyID string) (string, error) {
	if !validSegment(provider) || !validSegment(account) || !validSegment(keyID) {
		return "", application.ErrPaymentCallbackUnavailable
	}
	digest := sha256.Sum256([]byte("mender-payment-callback-secret-v1\x00" + provider + "\x00" + account + "\x00" + keyID))
	return hex.EncodeToString(digest[:]) + ".secret", nil
}

func contained(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p *Provider) ResolvePaymentCallbackSecret(ctx context.Context, provider, account, keyID string) (application.PaymentCallbackSecret, error) {
	if err := ctx.Err(); err != nil {
		return application.PaymentCallbackSecret{}, err
	}
	allowed, ok := p.keys[provider+"/"+account]
	if !ok {
		return application.PaymentCallbackSecret{}, application.ErrPaymentCallbackUnavailable
	}
	if _, ok = allowed[keyID]; !ok {
		return application.PaymentCallbackSecret{}, application.ErrPaymentCallbackUnavailable
	}
	name, err := FileName(provider, account, keyID)
	if err != nil {
		return application.PaymentCallbackSecret{}, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(p.root, name))
	if err != nil || !contained(p.root, resolved) {
		return application.PaymentCallbackSecret{}, application.ErrPaymentCallbackUnavailable
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > maxSecretBytes {
		return application.PaymentCallbackSecret{}, application.ErrPaymentCallbackUnavailable
	}
	file, err := os.Open(resolved)
	if err != nil {
		return application.PaymentCallbackSecret{}, application.ErrPaymentCallbackUnavailable
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	if err != nil || len(raw) < 32 || len(raw) > maxSecretBytes {
		return application.PaymentCallbackSecret{}, application.ErrPaymentCallbackUnavailable
	}
	return application.NewPaymentCallbackSecret(raw)
}

var _ application.PaymentCallbackSecretSource = (*Provider)(nil)
