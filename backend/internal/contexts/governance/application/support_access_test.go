package application

import (
	"context"
	"testing"
	"time"
)

type supportAuthStub struct{ action string }
func (a *supportAuthStub) Authenticate(context.Context, string) (Actor, error) { return Actor{UserID: "staff_a"}, nil }
func (a *supportAuthStub) AuthenticateMutation(context.Context, string, string) (Actor, error) { return Actor{UserID: "staff_a"}, nil }
func (a *supportAuthStub) AuthorizePlatform(_ context.Context, _ Actor, action string) error { a.action = action; return nil }

type supportIDsStub struct{}
func (supportIDsStub) NewDangerousOperationID() (string, error) { return "danger_support", nil }
func (supportIDsStub) NewJITGrantID() (string, error) { return "jit_support", nil }
type supportClockStub struct{ at time.Time }
func (c supportClockStub) Now() time.Time { return c.at }

type supportManagementStub struct{ requestAt, requestExpiry time.Time; scopes []string; ttl time.Duration }
func (r *supportManagementStub) RequestSupportJIT(_ context.Context, workspace, id, requester string, scopes []string, ttl time.Duration, reason string, at, expires time.Time) (DangerousOperationApproval, error) {
	r.requestAt, r.requestExpiry, r.scopes, r.ttl = at, expires, append([]string(nil), scopes...), ttl
	return DangerousOperationApproval{WorkspaceID: workspace, ID: id, RequesterUserID: requester, SubjectKind: "platform_staff", SubjectID: requester, Action: "support.workspace_read", TargetKind: "workspace", TargetID: workspace, ParametersJSON: `{"scopes": ["run:read"], "ttl_seconds": 600}`, ParametersSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Reason: reason, State: "pending", RequestedAt: at, ExpiresAt: expires}, nil
}
func (*supportManagementStub) ApproveDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error) { return DangerousOperationApproval{}, nil }
func (*supportManagementStub) RejectDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error) { return DangerousOperationApproval{}, nil }
func (*supportManagementStub) ActivateJITSupport(context.Context, string, string, string, string, time.Time) (JITSupportGrant, error) { return JITSupportGrant{}, nil }
func (*supportManagementStub) RevokeJITSupport(context.Context, string, string, string, time.Time, string) (JITSupportGrant, error) { return JITSupportGrant{}, nil }
type supportReadStub struct{ calls int }
func (r *supportReadStub) ListSupportRuns(context.Context, string, string, time.Time) ([]SupportRun, error) { r.calls++; return nil, nil }

func TestSupportRequestUsesPlatformAuthorityAndFixedApprovalTTL(t *testing.T) {
	at := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	management, reader, auth := &supportManagementStub{}, &supportReadStub{}, &supportAuthStub{}
	svc, err := NewSupportAccessService(management, reader, auth, supportIDsStub{}, supportClockStub{at})
	if err != nil { t.Fatal(err) }
	_, err = svc.Request(context.Background(), Actor{UserID: "staff_a"}, "ws_a", []string{"run:read"}, 10*time.Minute, "customer incident")
	if err != nil { t.Fatal(err) }
	if auth.action != "support:request" || !management.requestAt.Equal(at) || !management.requestExpiry.Equal(at.Add(15*time.Minute)) || management.ttl != 10*time.Minute || reader.calls != 0 {
		t.Fatal(auth.action, management, reader.calls)
	}
}

func TestSupportRequestRejectsMutationScopes(t *testing.T) {
	at := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	svc, _ := NewSupportAccessService(&supportManagementStub{}, &supportReadStub{}, &supportAuthStub{}, supportIDsStub{}, supportClockStub{at})
	for _, scopes := range [][]string{{"run:cancel"}, {"run:read", "run:read"}, {}} {
		if _, err := svc.Request(context.Background(), Actor{UserID: "staff_a"}, "ws_a", scopes, 10*time.Minute, "incident"); err != ErrInvalid {
			t.Fatal(scopes, err)
		}
	}
}
