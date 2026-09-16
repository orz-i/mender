package paymenthmac

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

type secretSource struct {
	secret application.PaymentCallbackSecret
}

func (s secretSource) ResolvePaymentCallbackSecret(context.Context, string, string, string) (application.PaymentCallbackSecret, error) {
	return s.secret, nil
}

func paymentSignature(secret []byte, provider, account string, at time.Time, body []byte) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("mender-payment-callback-v1\n" + provider + "\n" + account + "\n" + ts + "\n"))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestPaymentHMACBindsProviderAccountTimestampAndBody(t *testing.T) {
	rawSecret := []byte("0123456789abcdef0123456789abcdef")
	secret, err := application.NewPaymentCallbackSecret(rawSecret)
	if err != nil {
		t.Fatal(err)
	}
	verifier, _ := New(secretSource{secret: secret})
	at := time.Date(2026, 9, 16, 6, 30, 0, 0, time.UTC)
	body := []byte(`{"schema_version":1}`)
	timestamp := strconv.FormatInt(at.Unix(), 10)
	sig := paymentSignature(rawSecret, "sandbox_psp", "account.one", at, body)
	if _, err = verifier.VerifyPaymentCallback(context.Background(), "sandbox_psp", "account.one", "key_a", timestamp, sig, body, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		provider, account, signature string
		body                         []byte
	}{
		{"sandbox_other", "account.one", sig, body},
		{"sandbox_psp", "account.two", sig, body},
		{"sandbox_psp", "account.one", sig, []byte(`{"schema_version":2}`)},
		{"sandbox_psp", "account.one", "v1=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", body},
	} {
		if _, err = verifier.VerifyPaymentCallback(context.Background(), tc.provider, tc.account, "key_a", timestamp, tc.signature, tc.body, at.Add(time.Minute)); !errors.Is(err, application.ErrPaymentCallbackUnauthorized) {
			t.Fatal("invalid payment signature accepted", tc, err)
		}
	}
}
