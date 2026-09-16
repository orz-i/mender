package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type approvalReadRepo struct {
	dangerousRepoStub
	item DangerousOperationApproval
}

func (r *approvalReadRepo) GetDangerousOperation(context.Context, string, string) (DangerousOperationApproval, error) {
	return r.item, nil
}

type approvalReadAuth struct {
	dangerousAuthStub
	platform, workspace string
	denied              bool
}

func (a *approvalReadAuth) Authorize(_ context.Context, _ Actor, _ string, action string) error {
	a.workspace = action
	if a.denied {
		return ErrForbidden
	}
	return nil
}

func (a *approvalReadAuth) AuthorizePlatform(_ context.Context, _ Actor, action string) error {
	a.platform = action
	if a.denied {
		return ErrForbidden
	}
	return nil
}

func TestExactApprovalReadUsesActionScopedAuthorityWithoutTenantMembership(t *testing.T) {
	at := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	financialRepo := &dangerousRepoStub{}
	financial, err := financialRepo.RequestCommerceApproval(context.Background(), "ws_read", "approval_read", "maker_read", "commerce.refund", "refund_read", "usage_settlement", "run_read", "credit", 40, "USD", "reviewed refund", at, at.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	support := financial
	support.Action, support.TargetKind, support.TargetID, support.TargetVersion = "support.workspace_read", "workspace", "ws_read", ""
	support.AmountMicro, support.Currency = nil, ""
	support.ParametersJSON = `{"scopes":["run:read"],"ttl_seconds":900}`
	release := dangerousApproval("ws_read", "approval_read", "maker_read", "release_read", at, at.Add(15*time.Minute))
	for _, tt := range []struct {
		name, user, platform, workspace string
		item                            DangerousOperationApproval
	}{
		{"finance reviewer", "reviewer_read", "dangerous:review", "", financial},
		{"finance requester", "maker_read", "platform:operate", "", financial},
		{"support requester", "maker_read", "support:request", "", support},
		{"support reviewer", "reviewer_read", "dangerous:review", "", support},
		{"release tenant manager", "reviewer_read", "", "release:manage", release},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, auth := &approvalReadRepo{item: tt.item}, &approvalReadAuth{}
			svc, err := NewDangerousOperationService(repo, auth, dangerousIDStub{}, dangerousClockStub{at.Add(time.Minute)})
			if err != nil {
				t.Fatal(err)
			}
			item, err := svc.Get(context.Background(), Actor{UserID: tt.user}, "ws_read", "approval_read")
			if err != nil || item.ID != "approval_read" || auth.platform != tt.platform || auth.workspace != tt.workspace {
				t.Fatal(item, err, auth)
			}
			auth.denied = true
			item, err = svc.Get(context.Background(), Actor{UserID: "unauthorized"}, "ws_read", "approval_read")
			if !errors.Is(err, ErrForbidden) || item.ID != "" {
				t.Fatal("unauthorized projection returned", item, err)
			}
		})
	}
}

func TestExactApprovalReadRejectsMismatchedProjectionAndDerivesExpiryWithoutMutation(t *testing.T) {
	at := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	repo := &approvalReadRepo{item: dangerousApproval("ws_read", "approval_read", "maker_read", "release_read", at, at.Add(15*time.Minute))}
	auth := &approvalReadAuth{}
	svc, err := NewDangerousOperationService(repo, auth, dangerousIDStub{}, dangerousClockStub{at.Add(20 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.Get(context.Background(), Actor{UserID: "reviewer_read"}, "ws_read", "approval_read")
	if err != nil || item.State != "expired" || repo.item.State != "pending" {
		t.Fatal(item, err)
	}
	for _, ids := range [][2]string{{"ws_other", "approval_read"}, {"ws_read", "approval_other"}, {"../ws_read", "approval_read"}} {
		item, err = svc.Get(context.Background(), Actor{UserID: "reviewer_read"}, ids[0], ids[1])
		if err == nil || item.ID != "" {
			t.Fatal("mismatched binding accepted", ids, item)
		}
	}
}
