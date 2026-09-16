package random

import (
	"crypto/rand"
	"encoding/hex"
)

type PaymentIDs struct{}

func (PaymentIDs) NewPaymentIntentID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "pay_" + hex.EncodeToString(raw[:]), nil
}
