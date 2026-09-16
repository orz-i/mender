package random

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type ReleaseIDs struct{}

func (ReleaseIDs) NewID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "release_" + hex.EncodeToString(raw), nil
}

var _ application.PublicationIDGenerator = ReleaseIDs{}
