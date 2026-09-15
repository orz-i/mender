package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidPluginManifest = errors.New("invalid plugin manifest")

const PluginManifestAPIVersion = "mender.io/plugin/v1alpha1"

type CapabilityKind string

const (
	CapabilityAPITool CapabilityKind = "api_tool"
	CapabilityMCPTool CapabilityKind = "mcp_tool"
	CapabilityAgent   CapabilityKind = "agent"
)

type CapabilityReference struct {
	Kind               CapabilityKind `json:"kind"`
	ToolVersionID      string         `json:"tool_version_id,omitempty"`
	DeploymentRevision string         `json:"deployment_revision,omitempty"`
}

func (r CapabilityReference) Validate() error {
	switch r.Kind {
	case CapabilityAPITool, CapabilityMCPTool:
		if !validID(r.ToolVersionID) || r.DeploymentRevision != "" {
			return ErrInvalidPluginManifest
		}
	case CapabilityAgent:
		if !validID(r.DeploymentRevision) || r.ToolVersionID != "" {
			return ErrInvalidPluginManifest
		}
	default:
		return ErrInvalidPluginManifest
	}
	return nil
}

func (r CapabilityReference) identity() string {
	if r.ToolVersionID != "" {
		return string(r.Kind) + ":tool:" + r.ToolVersionID
	}
	return string(r.Kind) + ":deployment:" + r.DeploymentRevision
}

type PluginManifest struct {
	APIVersion   string                `json:"apiVersion"`
	PluginID     string                `json:"plugin_id"`
	Version      string                `json:"version"`
	PublisherID  string                `json:"publisher_id"`
	DisplayName  string                `json:"display_name"`
	Description  string                `json:"description"`
	Capabilities []CapabilityReference `json:"capabilities"`
}

func validManifestText(value string, maxRunes int, required bool) bool {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || len([]rune(value)) > maxRunes {
		return false
	}
	return !required || len([]rune(value)) > 0
}

func validPluginID(value string) bool {
	if len(value) < 3 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func validSemanticVersion(value string) bool {
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	if !hasPrerelease {
		return true
	}
	if prerelease == "" {
		return false
	}
	for i := 0; i < len(prerelease); i++ {
		ch := prerelease[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func (m PluginManifest) Validate() error {
	if m.APIVersion != PluginManifestAPIVersion || !validPluginID(m.PluginID) || !validSemanticVersion(m.Version) || !validID(m.PublisherID) {
		return ErrInvalidPluginManifest
	}
	if !validManifestText(m.DisplayName, 200, true) || !validManifestText(m.Description, 4000, false) || len(m.Capabilities) < 1 || len(m.Capabilities) > 64 {
		return ErrInvalidPluginManifest
	}
	seen := make(map[string]struct{}, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if capability.Validate() != nil {
			return ErrInvalidPluginManifest
		}
		identity := capability.identity()
		if _, duplicate := seen[identity]; duplicate {
			return ErrInvalidPluginManifest
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func (m PluginManifest) CanonicalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

type PluginVersionState string

const (
	PluginDraft      PluginVersionState = "draft"
	PluginSubmitted  PluginVersionState = "submitted"
	PluginApproved   PluginVersionState = "approved"
	PluginPublished  PluginVersionState = "published"
	PluginDeprecated PluginVersionState = "deprecated"
	PluginDisabled   PluginVersionState = "disabled"
)

func ValidPluginVersionState(state PluginVersionState) bool {
	switch state {
	case PluginDraft, PluginSubmitted, PluginApproved, PluginPublished, PluginDeprecated, PluginDisabled:
		return true
	default:
		return false
	}
}
