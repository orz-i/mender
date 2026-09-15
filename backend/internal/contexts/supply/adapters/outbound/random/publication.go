package random

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type PublicationIDs struct{}

func (PublicationIDs) NewID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "plugin_approval_" + hex.EncodeToString(raw), nil
}

var _ application.PublicationIDGenerator = PublicationIDs{}
