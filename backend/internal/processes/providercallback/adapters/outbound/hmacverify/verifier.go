package hmacverify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

type Verifier struct{ secrets application.SecretSource }

func New(secrets application.SecretSource) (*Verifier, error) {
	if secrets == nil {
		return nil, application.ErrUnavailable
	}
	return &Verifier{secrets: secrets}, nil
}

func parseSignature(value string) ([]byte, bool) {
	if len(value) != 67 || !strings.HasPrefix(value, "v1=") {
		return nil, false
	}
	digest, err := hex.DecodeString(value[3:])
	if err != nil || len(digest) != sha256.Size || strings.ToLower(value) != value {
		return nil, false
	}
	return digest, true
}

func signedTime(raw string) (time.Time, bool) {
	if len(raw) < 9 || len(raw) > 12 || raw[0] == '0' {
		return time.Time{}, false
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return time.Time{}, false
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}, false
	}
	return time.Unix(value, 0).UTC(), true
}

func (v *Verifier) Verify(ctx context.Context, provider, keyID, timestamp, signature string, raw []byte, receivedAt time.Time) (application.Verification, error) {
	if err := ctx.Err(); err != nil {
		return application.Verification{}, err
	}
	signedAt, ok := signedTime(timestamp)
	providedSignature, signatureOK := parseSignature(signature)
	if !ok || !signatureOK || receivedAt.IsZero() {
		return application.Verification{}, application.ErrUnauthorized
	}
	delta := receivedAt.Sub(signedAt)
	if delta < 0 {
		delta = -delta
	}
	if delta > 5*time.Minute {
		return application.Verification{}, application.ErrUnauthorized
	}
	secret, err := v.secrets.ResolveCallbackSecret(ctx, provider, keyID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.Verification{}, err
		}
		return application.Verification{}, application.ErrUnauthorized
	}
	mac := hmac.New(sha256.New, secret.Bytes())
	_, _ = mac.Write([]byte("mender-callback-v1\n" + timestamp + "\n"))
	_, _ = mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), providedSignature) {
		return application.Verification{}, application.ErrUnauthorized
	}
	digest := sha256.Sum256(raw)
	return application.Verification{SignedAt: signedAt, BodySHA256: hex.EncodeToString(digest[:])}, nil
}

var _ application.Verifier = (*Verifier)(nil)
