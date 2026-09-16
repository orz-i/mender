package paymenthmac

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

type Verifier struct {
	secrets application.PaymentCallbackSecretSource
}

func New(secrets application.PaymentCallbackSecretSource) (*Verifier, error) {
	if secrets == nil {
		return nil, application.ErrPaymentCallbackUnavailable
	}
	return &Verifier{secrets: secrets}, nil
}

func parseSignature(value string) ([]byte, bool) {
	if len(value) != 67 || !strings.HasPrefix(value, "v1=") {
		return nil, false
	}
	digest, err := hex.DecodeString(value[3:])
	return digest, err == nil && len(digest) == sha256.Size && strings.ToLower(value) == value
}

func parseTime(raw string) (time.Time, bool) {
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

func (v *Verifier) VerifyPaymentCallback(ctx context.Context, provider, account, keyID, timestamp, signature string, raw []byte, receivedAt time.Time) (application.PaymentCallbackVerification, error) {
	if err := ctx.Err(); err != nil {
		return application.PaymentCallbackVerification{}, err
	}
	signedAt, timeOK := parseTime(timestamp)
	provided, signatureOK := parseSignature(signature)
	if !timeOK || !signatureOK || receivedAt.IsZero() {
		return application.PaymentCallbackVerification{}, application.ErrPaymentCallbackUnauthorized
	}
	delta := receivedAt.Sub(signedAt)
	if delta < 0 {
		delta = -delta
	}
	if delta > 5*time.Minute {
		return application.PaymentCallbackVerification{}, application.ErrPaymentCallbackUnauthorized
	}
	secret, err := v.secrets.ResolvePaymentCallbackSecret(ctx, provider, account, keyID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.PaymentCallbackVerification{}, err
		}
		return application.PaymentCallbackVerification{}, application.ErrPaymentCallbackUnauthorized
	}
	mac := hmac.New(sha256.New, secret.Bytes())
	_, _ = mac.Write([]byte("mender-payment-callback-v1\n" + provider + "\n" + account + "\n" + timestamp + "\n"))
	_, _ = mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), provided) {
		return application.PaymentCallbackVerification{}, application.ErrPaymentCallbackUnauthorized
	}
	digest := sha256.Sum256(raw)
	return application.PaymentCallbackVerification{SignedAt: signedAt, BodySHA256: hex.EncodeToString(digest[:])}, nil
}

var _ application.PaymentCallbackVerifier = (*Verifier)(nil)
