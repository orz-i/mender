package domain

import (
	"testing"
	"time"
)

func TestDirectBindingRequiresStableAliasAndConnection(t *testing.T) {
	b := Binding{WorkspaceID: "ws_1", ToolsetVersionID: "set_1", ToolID: "tool_1", ToolVersionLabel: "1.0.0", ToolVersionID: "tv_1", BudgetID: "budget_1", ConnectionID: "conn_1", MCPName: "company_search", MCPExposed: true, State: "published", PublishedAt: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)}
	if !b.DirectCallable() {
		t.Fatal("valid direct MCP binding rejected")
	}
	for _, name := range []string{"", "CompanySearch", "company-search", "1search"} {
		candidate := b
		candidate.MCPName = name
		if candidate.DirectCallable() {
			t.Fatal("unstable MCP alias accepted", name)
		}
	}
	withoutConnection := b
	withoutConnection.ConnectionID = ""
	if withoutConnection.DirectCallable() {
		t.Fatal("direct binding accepted without a fixed Connection")
	}
}
