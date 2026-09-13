package domain

import (
	"testing"
	"time"
)

func TestCatalogWorkspaceAuthorization(t *testing.T) {
	roles := []struct {
		role      MembershipRole
		canManage bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleDeveloper, true},
		{RoleViewer, false},
	}
	for _, tc := range roles {
		membership := WorkspaceMembership{WorkspaceID: "ws_catalog", UserID: "user_catalog", Role: tc.role, CreatedAt: time.Now().UTC()}
		if !membership.Allows("catalog:read") {
			t.Fatal("active workspace role lost Catalog read", tc.role)
		}
		if got := membership.Allows("catalog:manage"); got != tc.canManage {
			t.Fatal("unexpected Catalog manage authorization", tc.role, got)
		}
	}
	disabled := WorkspaceMembership{WorkspaceID: "ws_catalog", UserID: "user_catalog", Role: RoleOwner, Disabled: true, CreatedAt: time.Now().UTC()}
	if disabled.Allows("catalog:read") || disabled.Allows("catalog:manage") {
		t.Fatal("disabled membership retained Catalog authority")
	}
}
