package domain

import (
	"testing"
	"time"
)

func TestPlatformStaffAuthorityDoesNotImplyTenantMembership(t *testing.T) {
	at := time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		role                   PlatformStaffRole
		request, review, audit bool
	}{
		{PlatformSupport, true, false, false},
		{PlatformReviewer, false, true, true},
		{PlatformOperator, true, true, true},
		{PlatformAuditor, false, false, true},
	}
	for _, tc := range cases {
		staff := PlatformStaff{UserID: "staff_a", Role: tc.role, CreatedAt: at}
		if !staff.Active() || staff.Allows("support:request") != tc.request || staff.Allows("dangerous:review") != tc.review || staff.Allows("dangerous:audit") != tc.audit || staff.Allows("release:manage") || staff.Allows("run:read") {
			t.Fatal("unexpected platform staff authority", tc.role)
		}
	}
	disabled := PlatformStaff{UserID: "staff_a", Role: PlatformOperator, Disabled: true, CreatedAt: at}
	if disabled.Active() || disabled.Allows("dangerous:review") {
		t.Fatal("disabled platform staff retained authority")
	}
}
