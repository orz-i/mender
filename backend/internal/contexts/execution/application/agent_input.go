package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrAgentInputInvalid     = errors.New("agent input request invalid")
	ErrAgentInputConflict    = errors.New("agent input request conflict")
	ErrAgentInputUnavailable = errors.New("agent input request unavailable")
)

type AgentInputRequest struct {
	WorkspaceID       domain.WorkspaceID
	RunID             domain.RunID
	AttemptNo         uint32
	ProviderID        string
	ProviderRequestID string
	ExternalTaskID    string
	InputRequestID    string
	Prompt            string
	InputSchemaJSON   string
	RequestedAt       time.Time
}

type AgentInputRequestRecord struct {
	WorkspaceID     string
	RunID           string
	InputRequestID  string
	State           string
	Prompt          string
	InputSchemaJSON string
	RequestedAt     time.Time
	UpdatedAt       time.Time
}

type AgentInputRequestRepository interface {
	RecordAgentInputRequest(context.Context, AgentInputRequest) (AgentInputRequestRecord, error)
}

func validAgentInputID(value string, max int, extended bool) bool {
	if len(value) < 1 || len(value) > max {
		return false
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' {
			continue
		}
		if extended && (ch == '.' || ch == ':') {
			continue
		}
		return false
	}
	return true
}

func validAgentInputHandle(value string) bool {
	if len(value) < 1 || len(value) > 512 || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func validAgentInputPrompt(value string) bool {
	if len(value) < 1 || len(value) > 2000 || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if ch == 0x7f || ch < 0x20 && ch != '\n' && ch != '\r' && ch != '\t' {
			return false
		}
	}
	return true
}

func validateAgentInputSchema(raw string) error {
	if len(raw) < 2 || len(raw) > 64<<10 {
		return ErrAgentInputInvalid
	}
	var value map[string]any
	if json.Unmarshal([]byte(raw), &value) != nil || value == nil {
		return ErrAgentInputInvalid
	}
	var visit func(any) bool
	visit = func(node any) bool {
		switch item := node.(type) {
		case map[string]any:
			for key, child := range item {
				if key == "$ref" {
					return false
				}
				if !visit(child) {
					return false
				}
			}
		case []any:
			for _, child := range item {
				if !visit(child) {
					return false
				}
			}
		}
		return true
	}
	if !visit(value) {
		return ErrAgentInputInvalid
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 64<<10 || !strings.HasPrefix(string(encoded), "{") {
		return ErrAgentInputInvalid
	}
	return nil
}

func (r AgentInputRequest) Validate() error {
	if !r.WorkspaceID.IsValid() || !r.RunID.IsValid() || r.AttemptNo < 1 || r.AttemptNo > 100 ||
		!validAgentInputID(r.ProviderID, 128, false) || !validAgentInputHandle(r.ProviderRequestID) || !validAgentInputHandle(r.ExternalTaskID) ||
		!validAgentInputID(r.InputRequestID, 200, true) || !validAgentInputPrompt(r.Prompt) || r.RequestedAt.IsZero() || r.RequestedAt.Year() < 1 || r.RequestedAt.Year() > 9999 || validateAgentInputSchema(r.InputSchemaJSON) != nil {
		return ErrAgentInputInvalid
	}
	return nil
}

type ProviderInputRequestSink interface {
	ObserveAgentInputRequest(context.Context, AgentInputRequest) (AgentInputRequestRecord, error)
}
