package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	execdomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	settlementapp "github.com/orz-i/mender/backend/internal/processes/settlement/application"
)

type fakeSettlementRunner struct {
	calls []string
	errAt string
}

type fakeOAuthRefreshRunner struct{ calls []string }

func (f *fakeOAuthRefreshRunner) RefreshOne(_ context.Context, workspace string) (connectionapp.OAuthRefreshReceipt, error) {
	f.calls = append(f.calls, workspace)
	if workspace == "ws_b" {
		return connectionapp.OAuthRefreshReceipt{}, connectionapp.ErrNoOAuthRefreshCandidate
	}
	return connectionapp.OAuthRefreshReceipt{ConnectionID: "conn_" + workspace, Revision: 2, Outcome: "refreshed"}, nil
}

func (f *fakeSettlementRunner) SettleOne(_ context.Context, workspace string) (settlementapp.Receipt, error) {
	f.calls = append(f.calls, workspace)
	if workspace == f.errAt {
		return settlementapp.Receipt{}, settlementapp.ErrUnavailable
	}
	if workspace == "ws_b" {
		return settlementapp.Receipt{}, settlementapp.ErrNoWork
	}
	return settlementapp.Receipt{ChargedMicro: 7}, nil
}

func TestReviewedRuntimeCycleBoundsControlAndSettlementPerWorkspace(t *testing.T) {
	cancel := &fakeControlCancel{err: execapp.ErrNoProviderCancellation}
	status := &fakeControlStatus{err: execapp.ErrNoProviderReconciliation}
	control, err := NewReviewedProviderControlRuntime(cancel, status)
	if err != nil {
		t.Fatal(err)
	}
	settlement := &fakeSettlementRunner{}
	oauthRefresh := &fakeOAuthRefreshRunner{}
	services, err := NewReviewedWorkerServicesWithOAuthRefresh(nil, control, &ReviewedUsageSettlementRuntime{service: settlement}, &ReviewedOAuthRefreshRuntime{service: oauthRefresh})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunReviewedRuntimeCycle(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), []execdomain.WorkspaceID{"ws_a", "ws_b"}, services)
	if err != nil || result.ProviderControlCycles != 2 || result.ProviderCancellationsHandled != 0 || result.ProviderReconciliationsHandled != 0 || result.SettlementsHandled != 1 || result.OAuthRefreshesHandled != 1 || cancel.calls != 2 || status.calls != 2 || len(settlement.calls) != 2 || len(oauthRefresh.calls) != 2 {
		t.Fatal(result, cancel.calls, status.calls, settlement.calls, oauthRefresh.calls, err)
	}
}

func TestReviewedRuntimeCycleFailsClosedOnInvalidScopeOrBackgroundFailure(t *testing.T) {
	settlement := &fakeSettlementRunner{errAt: "ws_b"}
	services, err := NewReviewedWorkerServices(nil, nil, &ReviewedUsageSettlementRuntime{service: settlement})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err = RunReviewedRuntimeCycle(context.Background(), logger, []execdomain.WorkspaceID{"bad workspace"}, services); err == nil || len(settlement.calls) != 0 {
		t.Fatal("invalid workspace reached reviewed background service", err, settlement.calls)
	}
	result, err := RunReviewedRuntimeCycle(context.Background(), logger, []execdomain.WorkspaceID{"ws_a", "ws_b", "ws_c"}, services)
	if !errors.Is(err, settlementapp.ErrUnavailable) || result.SettlementsHandled != 1 || len(settlement.calls) != 2 {
		t.Fatal("background failure did not stop bounded cycle", result, settlement.calls, err)
	}
	if runtime, err := NewReviewedWorkerServices(nil, nil, nil); err == nil || runtime != nil {
		t.Fatal("empty reviewed service set was accepted")
	}
}
