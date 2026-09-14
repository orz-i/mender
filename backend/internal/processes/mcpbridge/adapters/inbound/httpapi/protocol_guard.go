package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var errProtocolBodyMismatch = errors.New("mcp initialize protocol version does not match certified transport")

// enforceInitializeProtocolVersion keeps Mender's certified HTTP protocol
// boundary authoritative before authentication and before the SDK sees JSON-RPC.
// It does not implement initialize: malformed/non-initialize requests are still
// owned by the SDK. The body is restored byte-for-byte after the bounded check.
func enforceInitializeProtocolVersion(r *http.Request) error {
	if r == nil || r.Body == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 || len(body) > maxRequestBody {
		return nil
	}
	var envelope struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil || envelope.Method != "initialize" || len(envelope.Params) == 0 {
		return nil
	}
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err = json.Unmarshal(envelope.Params, &params); err != nil || params.ProtocolVersion == "" {
		return nil
	}
	if params.ProtocolVersion != ProtocolVersion {
		return errProtocolBodyMismatch
	}
	return nil
}
