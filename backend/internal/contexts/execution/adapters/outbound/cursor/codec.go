package cursor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

var ErrInvalid = errors.New("invalid cursor encoding")

type Codec struct{ key [32]byte }

// A shared, server-managed secret is required across replicas. Tokens are signed,
// not encrypted; never put secrets or raw user payloads in them.
func New(key []byte) (*Codec, error) {
	if len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
		return nil, errors.New("cursor signing key must contain 32 bytes and cannot be all zero")
	}
	c := &Codec{}
	copy(c.key[:], key)
	return c, nil
}

func (c *Codec) Encode(value ports.Cursor) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalid
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(body)
	token := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > 2048 {
		return "", ErrInvalid
	}
	return token, nil
}

func (c *Codec) Decode(token string) (ports.Cursor, error) {
	var value ports.Cursor
	parts := strings.Split(token, ".")
	if len(token) > 2048 || len(parts) != 2 {
		return value, ErrInvalid
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return value, ErrInvalid
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(sig) != sha256.Size {
		return value, ErrInvalid
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return value, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&value); err != nil {
		return ports.Cursor{}, ErrInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, body) {
		return ports.Cursor{}, ErrInvalid
	}
	return value, nil
}

var _ ports.CursorCodec = (*Codec)(nil)
