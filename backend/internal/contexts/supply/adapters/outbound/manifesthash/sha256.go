package manifesthash

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type SHA256 struct{}

func New() SHA256 { return SHA256{} }

func (SHA256) SHA256(value []byte) (string, error) {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:]), nil
}

var _ application.ManifestDigester = SHA256{}
