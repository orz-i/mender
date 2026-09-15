package agentinput

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/sharedkernel/canonicaljson"
)

type Preparer struct{}

func normalizeNumbers(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		raw := value.String()
		if !strings.ContainsAny(raw, ".eE") {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return n, nil
			}
			if !strings.HasPrefix(raw, "-") {
				if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
					return n, nil
				}
			}
		}
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, application.ErrAgentInputInvalid
		}
		return n, nil
	case map[string]any:
		for key, child := range value {
			normalized, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			value[key] = normalized
		}
		return value, nil
	case []any:
		for i, child := range value {
			normalized, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			value[i] = normalized
		}
		return value, nil
	default:
		return value, nil
	}
}

func (Preparer) PrepareAgentInput(schemaJSON, inputRequestID string, raw []byte) (application.PreparedAgentInput, error) {
	if len(raw) < 2 || len(raw) > 64<<10 || len(inputRequestID) < 1 || len(inputRequestID) > 200 {
		return application.PreparedAgentInput{}, application.ErrAgentInputInvalid
	}
	canonical, err := canonicaljson.Object(raw, 64<<10)
	if err != nil {
		return application.PreparedAgentInput{}, application.ErrAgentInputInvalid
	}
	var schema jsonschema.Schema
	if json.Unmarshal([]byte(schemaJSON), &schema) != nil {
		return application.PreparedAgentInput{}, application.ErrAgentInputUnavailable
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return application.PreparedAgentInput{}, application.ErrAgentInputUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	var instance any
	if decoder.Decode(&instance) != nil {
		return application.PreparedAgentInput{}, application.ErrAgentInputInvalid
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return application.PreparedAgentInput{}, application.ErrAgentInputInvalid
	}
	instance, err = normalizeNumbers(instance)
	if err != nil || resolved.Validate(instance) != nil {
		return application.PreparedAgentInput{}, application.ErrAgentInputInvalid
	}
	digest := sha256.Sum256(canonical)
	answerSHA := hex.EncodeToString(digest[:])
	submissionDigest := sha256.Sum256([]byte("mender-agent-input-v1\x00" + inputRequestID + "\x00" + answerSHA))
	prepared := application.PreparedAgentInput{
		AnswerJSON: string(canonical), AnswerSHA256: answerSHA,
		SubmissionID: "input." + hex.EncodeToString(submissionDigest[:]),
	}
	if !prepared.Valid() {
		return application.PreparedAgentInput{}, application.ErrAgentInputUnavailable
	}
	return prepared, nil
}

var _ application.AgentInputPreparer = Preparer{}
