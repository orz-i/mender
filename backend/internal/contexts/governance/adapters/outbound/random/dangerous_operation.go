package random

import (
	"crypto/rand"
	"encoding/hex"
)

type DangerousOperationIDs struct{}

func (DangerousOperationIDs) NewDangerousOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "danger_" + hex.EncodeToString(value[:]), nil
}
