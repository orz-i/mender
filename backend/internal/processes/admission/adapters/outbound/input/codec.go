package input

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/orz-i/mender/backend/internal/platform/canonicaljson"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Codec struct{}

func (Codec) Prepare(q application.Request) (application.Prepared, error) {
	canonical, e := canonicaljson.Object(q.Arguments, 65536)
	if e != nil {
		return application.Prepared{}, application.ErrInvalid
	}
	// Stable structured fields, not delimiter concatenation. Version makes future rules explicit.
	content, e := json.Marshal(struct {
		Version                                                 int
		ToolID, ToolVersion, Toolset, Connection, Currency, Cap string
		Arguments                                               json.RawMessage
	}{2, q.ToolID, q.ToolVersion, q.ToolsetVersionID, q.ConnectionID, q.Currency, q.MaxChargeMicro, canonical})
	if e != nil {
		return application.Prepared{}, application.ErrInvalid
	}
	sum := sha256.Sum256(content)
	return application.Prepared{
		CanonicalArguments: string(canonical), RequestHash: hex.EncodeToString(sum[:]),
		ArgumentsHash: canonicaljson.SHA256(canonical), IdempotencyKeyHash: canonicaljson.SHA256([]byte(q.IdempotencyKey)),
	}, nil
}

type IDs struct{}

func (IDs) NewRunID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return "run_" + hex.EncodeToString(b[:]), nil
}
