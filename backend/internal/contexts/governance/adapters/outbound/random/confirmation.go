package random

import (
	"crypto/rand"
	"encoding/hex"
)

type ConfirmationIDs struct{}

func (ConfirmationIDs) NewConfirmationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "confirm_" + hex.EncodeToString(value[:]), nil
}
