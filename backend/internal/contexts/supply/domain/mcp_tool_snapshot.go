package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidMCPToolSnapshot = errors.New("invalid MCP tool snapshot")

type MCPToolSnapshot struct {
	DeploymentRevision string
	ToolName           string
	Title              string
	Description        string
	InputSchema        string
	OutputSchema       string
	AnnotationsJSON    string
	ContentSHA256      string
	DiscoveredAt       time.Time
}

func validMCPToolName(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func jsonObject(raw string, required bool) bool {
	if raw == "" {
		return !required
	}
	if len(raw) > 1<<20 || !json.Valid([]byte(raw)) {
		return false
	}
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil && value != nil
}

func (s MCPToolSnapshot) Validate() error {
	if !validID(s.DeploymentRevision) || !validMCPToolName(s.ToolName) || len([]rune(s.Title)) > 200 || len([]rune(s.Description)) > 4000 || strings.ContainsRune(s.Title, 0) || strings.ContainsRune(s.Description, 0) || !jsonObject(s.InputSchema, true) || !jsonObject(s.OutputSchema, false) || !jsonObject(s.AnnotationsJSON, true) || len(s.ContentSHA256) != 64 || s.DiscoveredAt.IsZero() {
		return ErrInvalidMCPToolSnapshot
	}
	for _, c := range s.ContentSHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ErrInvalidMCPToolSnapshot
		}
	}
	return nil
}
