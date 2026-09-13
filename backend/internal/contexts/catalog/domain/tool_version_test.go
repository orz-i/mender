package domain

import (
	"testing"
	"time"
)

func validContract() ToolVersion {
	return ToolVersion{ID: "tv_1", ToolID: "tool_1", Version: "1.0.0", ProviderID: "provider_1", PriceVersionID: "price_1", DeploymentRevision: "deploy_1", Title: "Search companies", Description: "Searches reviewed company data.", InputSchema: `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}}}`, OutputSchema: `{"type":"object","properties":{"items":{"type":"array"}}}`, SideEffect: "read_only", Idempotency: "safe_read", MCPPublishable: true, State: "published", PublishedAt: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)}
}

func TestRetiredToolVersionIsValidButNotCallable(t *testing.T) {
	v := validContract()
	v.State = "retired"
	if err := v.Validate(); err != nil {
		t.Fatal("retired immutable tool contract rejected", err)
	}
	if v.Callable() || v.DirectPublishable() {
		t.Fatal("retired ToolVersion remained callable")
	}
}

func TestToolVersionContractIsRequiredForCallableVersion(t *testing.T) {
	v := validContract()
	if err := v.Validate(); err != nil || !v.Callable() {
		t.Fatal("valid immutable tool contract rejected", err)
	}
	if !v.DirectPublishable() {
		t.Fatal("reviewed MCP contract is not directly publishable")
	}
	legacy := v
	legacy.MCPPublishable = false
	legacy.Title = ""
	if !legacy.Callable() || legacy.DirectPublishable() {
		t.Fatal("legacy callable contract was implicitly exposed to direct MCP")
	}
	bad := []ToolVersion{v, v, v, v}
	bad[0].InputSchema = `{"type":"array"}`
	bad[1].OutputSchema = `[]`
	bad[2].SideEffect = "unknown"
	bad[3].Title = ""
	for i, item := range bad {
		if item.DirectPublishable() {
			t.Fatal("invalid tool contract accepted", i)
		}
	}
}
