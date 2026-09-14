package hmacverify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

type secrets struct{ secret application.Secret }

func (s secrets) ResolveCallbackSecret(context.Context, string, string) (application.Secret, error) {
	return s.secret, nil
}

func signature(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("mender-callback-v1\n" + timestamp + "\n"))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifierAuthenticatesExactRawBodyAndTimestampWindow(t *testing.T) {
	rawSecret := []byte("0123456789abcdef0123456789abcdef")
	secret, err := application.NewSecret(rawSecret)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := New(secrets{secret})
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 9, 14, 3, 2, 0, 0, time.UTC)
	body := []byte(`{"a":1}`)
	timestamp := "1789354800"
	result, err := verifier.Verify(context.Background(), "provider_a", "key_a", timestamp, signature(rawSecret, timestamp, body), body, when)
	if err != nil || !result.SignedAt.Equal(time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)) || len(result.BodySHA256) != 64 {
		t.Fatal(result, err)
	}
	if _, err = verifier.Verify(context.Background(), "provider_a", "key_a", timestamp, signature(rawSecret, timestamp, append(body, ' ')), body, when); !errors.Is(err, application.ErrUnauthorized) {
		t.Fatal("raw-body tamper was accepted", err)
	}
	if _, err = verifier.Verify(context.Background(), "provider_a", "key_a", "1789350000", signature(rawSecret, "1789350000", body), body, when); !errors.Is(err, application.ErrUnauthorized) {
		t.Fatal("stale callback timestamp was accepted", err)
	}
}
