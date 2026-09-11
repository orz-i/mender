package application

import (
	"context"
	"encoding/json"
)

// DirectTool is the stable, secret-free Toolset publication projection. It
// contains only server-owned routing facts and immutable user-facing schemas.
type DirectTool struct {
	Name, Title, Description                           string
	ToolID, ToolVersion, ToolVersionID, ConnectionID   string
	InputSchema, OutputSchema, SideEffect, Idempotency string
}

type DirectTools interface {
	List(context.Context, Caller, string) ([]DirectTool, error)
	Resolve(context.Context, Caller, string, string) (DirectTool, error)
}

type FixedService struct {
	auth    Authenticator
	starter Starter
	tools   DirectTools
}

func NewFixed(auth Authenticator, starter Starter, tools DirectTools) (*FixedService, error) {
	if auth == nil || starter == nil || tools == nil {
		return nil, ErrUnavailable
	}
	return &FixedService{auth: auth, starter: starter, tools: tools}, nil
}

func (s *FixedService) Authenticate(ctx context.Context, token, workspace string) (Caller, error) {
	return authenticate(ctx, s.auth, token, workspace)
}

func (s *FixedService) ListTools(ctx context.Context, caller Caller, toolsetID string) ([]DirectTool, error) {
	if !validID(toolsetID) {
		return nil, ErrInvalid
	}
	items, err := s.tools.List(ctx, caller, toolsetID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !validDirectTool(item) {
			return nil, ErrUnavailable
		}
		if _, exists := seen[item.Name]; exists {
			return nil, ErrUnavailable
		}
		seen[item.Name] = struct{}{}
	}
	return items, nil
}

func (s *FixedService) StartTool(ctx context.Context, caller Caller, toolsetID, name, idempotencyKey, currency, maxCharge string, arguments []byte) (StartReceipt, error) {
	if !validID(toolsetID) || !validMCPName(name) || len(arguments) < 2 || len(arguments) > 65536 || !json.Valid(arguments) || arguments[0] != '{' {
		return StartReceipt{}, ErrInvalid
	}
	tool, err := s.tools.Resolve(ctx, caller, toolsetID, name)
	if err != nil {
		return StartReceipt{}, err
	}
	if !validDirectTool(tool) {
		return StartReceipt{}, ErrUnavailable
	}
	return s.starter.Start(ctx, caller, StartRequest{
		IdempotencyKey: idempotencyKey,
		ToolID:         tool.ToolID, ToolVersion: tool.ToolVersion,
		ToolsetVersionID: toolsetID, ConnectionID: tool.ConnectionID,
		Arguments: arguments, Currency: currency, MaxChargeMicro: maxCharge,
	})
}

func validDirectTool(tool DirectTool) bool {
	return validMCPName(tool.Name) && validID(tool.ToolID) && validToolVersion(tool.ToolVersion) && validID(tool.ToolVersionID) && validID(tool.ConnectionID) && tool.Title != "" && len(tool.InputSchema) > 1 && len(tool.OutputSchema) > 1 && json.Valid([]byte(tool.InputSchema)) && json.Valid([]byte(tool.OutputSchema)) && (tool.SideEffect == "read_only" || tool.SideEffect == "write") && (tool.Idempotency == "safe_read" || tool.Idempotency == "idempotent" || tool.Idempotency == "unsafe")
}

func validToolVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, c := range value {
		if i == 0 && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func validMCPName(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
