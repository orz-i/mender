package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidPluginManifest = errors.New("invalid plugin manifest")
	pluginIDPattern          = regexp.MustCompile(`^[a-z][a-z0-9.-]{2,127}$`)
	semanticVersionPattern   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
)

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

func (m PluginManifest) Validate() error {
	if m.APIVersion != PluginManifestAPIVersion || !pluginIDPattern.MatchString(m.PluginID) || !semanticVersionPattern.MatchString(m.Version) || !validID(m.PublisherID) {
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

func (m PluginManifest) SHA256() (string, error) {
	body, err := m.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
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
