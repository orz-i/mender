package random

import (
	"crypto/rand"
	"encoding/hex"
)

type Generator struct{}

func (Generator) NewID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "approval_" + hex.EncodeToString(raw), nil
}
