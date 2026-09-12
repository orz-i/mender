package sessioncodec

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

type Codec struct{}

func (Codec) Generate() (string, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", errors.New("session randomness unavailable")
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func (Codec) Digest(raw string) (string, error) {
	if len(raw) != 43 {
		return "", errors.New("invalid session token")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return "", errors.New("invalid session token")
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), nil
}

func (Codec) EqualDigest(left, right string) bool {
	return len(left) == 64 && len(right) == 64 && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (Codec) EqualRaw(left, right string) bool {
	return len(left) == len(right) && len(left) > 0 && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
