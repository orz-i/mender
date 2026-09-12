package random

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
)

type Generator struct{}

func (Generator) Token(bytes int) (string, error) {
	if bytes < 16 || bytes > 64 {
		return "", errors.New("random token size is invalid")
	}
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

var _ application.OAuthRandom = Generator{}
