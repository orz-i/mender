package domain

import (
	"testing"
	"time"
)

func TestPlatformStaffAuthorityDoesNotImplyTenantMembership(t *testing.T) {
	at := time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		role                                           PlatformStaffRole
		request, dangerousReview, dangerousAudit       bool
		platformOperate, platformReview, platformAudit bool
	}{
		{PlatformSupport, true, false, false, false, false, false},
		{PlatformReviewer, false, true, true, false, true, true},
		{PlatformOperator, true, true, true, true, true, true},
		{PlatformAuditor, false, false, true, false, false, true},
	}
	for _, tc := range cases {
		staff := PlatformStaff{UserID: "staff_a", Role: tc.role, CreatedAt: at}
		if !staff.Active() || staff.Allows("support:request") != tc.request || staff.Allows("dangerous:review") != tc.dangerousReview || staff.Allows("dangerous:audit") != tc.dangerousAudit ||
			staff.Allows("platform:operate") != tc.platformOperate || staff.Allows("platform:review") != tc.platformReview || staff.Allows("platform:audit") != tc.platformAudit ||
			staff.Allows("release:manage") || staff.Allows("run:read") {
			t.Fatal("unexpected platform staff authority", tc.role)
		}
	}
	disabled := PlatformStaff{UserID: "staff_a", Role: PlatformOperator, Disabled: true, CreatedAt: at}
	if disabled.Active() || disabled.Allows("dangerous:review") || disabled.Allows("platform:operate") {
		t.Fatal("disabled platform staff retained authority")
	}
}
