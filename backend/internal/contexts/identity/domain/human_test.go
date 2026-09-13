package domain

import (
	"testing"
	"time"
)

func TestCatalogWorkspaceAuthorization(t *testing.T) {
	createdAt := time.Date(2026, 9, 12, 20, 0, 0, 0, time.UTC)
	roles := []struct {
		role      MembershipRole
		canManage bool
		canReview bool
	}{
		{RoleOwner, true, true},
		{RoleAdmin, true, true},
		{RoleDeveloper, true, false},
		{RoleViewer, false, false},
	}
	for _, tc := range roles {
		membership := WorkspaceMembership{WorkspaceID: "ws_catalog", UserID: "user_catalog", Role: tc.role, CreatedAt: createdAt}
		if !membership.Allows("catalog:read") {
			t.Fatal("active workspace role lost Catalog read", tc.role)
		}
		if got := membership.Allows("catalog:manage"); got != tc.canManage {
			t.Fatal("unexpected Catalog manage authorization", tc.role, got)
		}
		if got := membership.Allows("catalog:review"); got != tc.canReview {
			t.Fatal("unexpected Catalog review authorization", tc.role, got)
		}
	}
	disabled := WorkspaceMembership{WorkspaceID: "ws_catalog", UserID: "user_catalog", Role: RoleOwner, Disabled: true, CreatedAt: createdAt}
	if disabled.Allows("catalog:read") || disabled.Allows("catalog:manage") || disabled.Allows("catalog:review") {
		t.Fatal("disabled membership retained Catalog authority")
	}
}
