package artifactcap

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
)

var ErrInvalid = errors.New("invalid artifact object capability")

type Codec struct{ key [32]byte }

func New(key []byte) (*Codec, error) {
	if len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
		return nil, errors.New("artifact capability signing key must contain 32 non-zero bytes")
	}
	c := &Codec{}
	copy(c.key[:], key)
	return c, nil
}

func NewFromFile(path string) (*Codec, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("artifact capability signing key file is required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != 32 {
		return nil, errors.New("artifact capability signing key file is invalid")
	}
	key, err := os.ReadFile(path)
	if err != nil || len(key) != 32 {
		return nil, errors.New("artifact capability signing key file is unavailable")
	}
	return New(key)
}

func (c *Codec) EncodeArtifactObjectCapability(value application.ArtifactObjectCapability) (string, error) {
	if c == nil {
		return "", ErrInvalid
	}
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

func (c *Codec) DecodeArtifactObjectCapability(token string) (application.ArtifactObjectCapability, error) {
	var value application.ArtifactObjectCapability
	if c == nil || len(token) < 1 || len(token) > 2048 {
		return value, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return value, ErrInvalid
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return value, ErrInvalid
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return value, ErrInvalid
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return value, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&value); err != nil {
		return application.ArtifactObjectCapability{}, ErrInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, body) {
		return application.ArtifactObjectCapability{}, ErrInvalid
	}
	return value, nil
}

var _ application.ArtifactObjectCapabilityCodec = (*Codec)(nil)
