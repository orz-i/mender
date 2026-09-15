package domain

import "testing"

func validManifest() PluginManifest {
	return PluginManifest{
		APIVersion:  PluginManifestAPIVersion,
		PluginID:    "example.search",
		Version:     "1.2.3",
		PublisherID: "publisher_example",
		DisplayName: "Example search",
		Description: "Reviewed capability references only.",
		Capabilities: []CapabilityReference{
			{Kind: CapabilityAPITool, ToolVersionID: "toolv_api_1"},
			{Kind: CapabilityMCPTool, ToolVersionID: "toolv_mcp_1"},
			{Kind: CapabilityAgent, DeploymentRevision: "deployment_agent_1"},
		},
	}
}

func TestPluginManifestValidatesControlledReferences(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	body, err := m.CanonicalJSON()
	if err != nil || len(body) == 0 {
		t.Fatalf("canonical manifest unavailable: %v", err)
	}
	digest, err := m.SHA256()
	if err != nil || len(digest) != 64 {
		t.Fatalf("manifest digest unavailable: %q %v", digest, err)
	}
	digest2, _ := m.SHA256()
	if digest != digest2 {
		t.Fatal("manifest digest is not deterministic")
	}
}

func TestPluginManifestRejectsUncontrolledOrAmbiguousReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PluginManifest)
	}{
		{"unknown kind", func(m *PluginManifest) { m.Capabilities[0].Kind = "script" }},
		{"api deployment ref", func(m *PluginManifest) { m.Capabilities[0].DeploymentRevision = "deployment_1" }},
		{"agent tool ref", func(m *PluginManifest) { m.Capabilities[2].ToolVersionID = "toolv_1" }},
		{"duplicate ref", func(m *PluginManifest) { m.Capabilities = append(m.Capabilities, m.Capabilities[0]) }},
		{"bad plugin id", func(m *PluginManifest) { m.PluginID = "Bad Plugin" }},
		{"bad version", func(m *PluginManifest) { m.Version = "latest" }},
		{"empty capabilities", func(m *PluginManifest) { m.Capabilities = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			tt.mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestPluginVersionStatesAreExplicit(t *testing.T) {
	for _, state := range []PluginVersionState{PluginDraft, PluginSubmitted, PluginApproved, PluginPublished, PluginDeprecated, PluginDisabled} {
		if !ValidPluginVersionState(state) {
			t.Fatalf("state %q rejected", state)
		}
	}
	if ValidPluginVersionState("reconciling") {
		t.Fatal("unknown plugin publication state accepted")
	}
}
