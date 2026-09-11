package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidToolVersion = errors.New("invalid tool version")

type ToolVersion struct {
	ID, ToolID, Version, ProviderID, PriceVersionID, DeploymentRevision string
	Title, Description, InputSchema, OutputSchema                       string
	SideEffect, Idempotency                                             string
	MCPPublishable                                                      bool
	State                                                               string
	PublishedAt                                                         time.Time
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, c := range value {
		if i == 0 && !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == ':' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func (v ToolVersion) Validate() error {
	if !validID(v.ID) || !validID(v.ToolID) || !validVersion(v.Version) || !validID(v.ProviderID) || !validID(v.PriceVersionID) || !validID(v.DeploymentRevision) || (v.State != "published" && v.State != "disabled") || v.PublishedAt.IsZero() {
		return ErrInvalidToolVersion
	}
	if len([]rune(v.Title)) < 1 || len([]rune(v.Title)) > 200 || len([]rune(v.Description)) > 4000 || strings.ContainsRune(v.Title, 0) || strings.ContainsRune(v.Description, 0) {
		return ErrInvalidToolVersion
	}
	if v.SideEffect != "read_only" && v.SideEffect != "write" {
		return ErrInvalidToolVersion
	}
	if v.Idempotency != "safe_read" && v.Idempotency != "idempotent" && v.Idempotency != "unsafe" {
		return ErrInvalidToolVersion
	}
	if !objectSchema(v.InputSchema, true) || !objectSchema(v.OutputSchema, false) {
		return ErrInvalidToolVersion
	}
	return nil
}

func (v ToolVersion) Callable() bool { return v.Validate() == nil && v.State == "published" }

func (v ToolVersion) DirectPublishable() bool { return v.Callable() && v.MCPPublishable }

func objectSchema(raw string, requireObjectType bool) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || !json.Valid([]byte(raw)) {
		return false
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil || value == nil {
		return false
	}
	if requireObjectType {
		kind, ok := value["type"].(string)
		return ok && kind == "object"
	}
	return true
}
