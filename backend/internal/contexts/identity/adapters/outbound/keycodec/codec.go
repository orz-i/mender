package keycodec

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Keys have 256 random secret bits. SHA-256 here verifies random machine credentials,
// not human passwords; never use this codec to store a user password.
type Codec struct{}

func (Codec) Generate() (token, id, digest string, err error) {
	var keyID [16]byte
	var secret [32]byte
	if _, err = rand.Read(keyID[:]); err != nil {
		return
	}
	if _, err = rand.Read(secret[:]); err != nil {
		return
	}
	id = hex.EncodeToString(keyID[:])
	token = "mender_live_" + id + "." + base64.RawURLEncoding.EncodeToString(secret[:])
	_, digest, err = (Codec{}).Parse(token)
	return
}

func (Codec) Parse(token string) (string, string, error) {
	bad := errors.New("invalid machine credential")
	if len(token) != 88 || !strings.HasPrefix(token, "mender_live_") {
		return "", "", bad
	}
	parts := strings.Split(token[len("mender_live_"):], ".")
	if len(parts) != 2 || len(parts[0]) != 32 {
		return "", "", bad
	}
	id, err := hex.DecodeString(parts[0])
	if err != nil || len(id) != 16 || hex.EncodeToString(id) != parts[0] {
		return "", "", bad
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(secret) != 32 {
		return "", "", bad
	}
	sum := sha256.Sum256([]byte(token))
	return parts[0], hex.EncodeToString(sum[:]), nil
}

func (Codec) EqualDigest(a, b string) bool {
	return len(a) == 64 && len(b) == 64 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
