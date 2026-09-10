package execution_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

func TestCursorIntegrityKeyIsolationAndCanonicalEncoding(t *testing.T) {
	codec := queryCodec(t)
	value := ports.Cursor{Format: 1, Kind: "runs", Workspace: "ws_a", Subject: "subject_a", Credential: "key_a", Size: 20, ExpiresAt: at}
	token, e := codec.Encode(value)
	if e != nil {
		t.Fatal(e)
	}
	got, e := codec.Decode(token)
	if e != nil || got != value {
		t.Fatal(got, e)
	}
	other, e := cursor.New([]byte(strings.Repeat("r", 32)))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Decode(token); e == nil {
		t.Fatal("another signing key accepted token")
	}
	for _, bad := range []string{"", token + "x", token + ".", strings.Split(token, ".")[0] + ".AA", strings.Repeat("a", 2049), "abc.def"} {
		if _, e = codec.Decode(bad); e == nil {
			t.Fatal("malformed token accepted")
		}
	}
	for _, body := range []string{`{"v":1,"v":1}`, `{"unexpected":true}`, `{} {}`, `null`} {
		mac := hmac.New(sha256.New, []byte(strings.Repeat("q", 32)))
		_, _ = mac.Write([]byte(body))
		signed := base64.RawURLEncoding.EncodeToString([]byte(body)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if _, e = codec.Decode(signed); e == nil {
			t.Fatal("noncanonical signed cursor accepted")
		}
	}
	for _, key := range [][]byte{nil, make([]byte, 32), []byte("short"), []byte(strings.Repeat("a", 33))} {
		if _, e = cursor.New(key); e == nil {
			t.Fatal("invalid signing key accepted")
		}
	}
}
