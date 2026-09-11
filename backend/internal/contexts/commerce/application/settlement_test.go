package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

type settlementRepoFake struct {
	locked      LockedSettlement
	lockErr     error
	storeErr    error
	storeCalls  int
	lastRequest SettlementRequest
	lastState   domain.AllowanceSnapshot
	lastRev     int64
	lastCharge  int64
}

func (f *settlementRepoFake) LockSettlement(context.Context, SettlementRequest) (LockedSettlement, error) {
	return f.locked, f.lockErr
}

func (f *settlementRepoFake) StoreSettlement(_ context.Context, q SettlementRequest, allowance domain.Allowance, rev, charged int64) (SettlementReceipt, error) {
	f.storeCalls++
	f.lastRequest, f.lastState, f.lastRev, f.lastCharge = q, allowance.Snapshot(), rev, charged
	if f.storeErr != nil {
		return SettlementReceipt{}, f.storeErr
	}
	return SettlementReceipt{WorkspaceID: q.WorkspaceID, RunID: q.RunID, ReservationID: q.ReservationID, PriceVersionID: q.PriceVersionID, BudgetID: q.BudgetID, PeriodID: q.PeriodID, Currency: q.Currency, ReservedMicro: q.ReservedMicro, ChargedMicro: charged, Outcome: q.Outcome, ObservedAt: q.ObservedAt, SettledAt: q.SettledAt}, nil
}

func settlementFixture(t *testing.T, outcome domain.SettlementOutcome) (*SettlementService, *settlementRepoFake, SettlementRequest) {
	t.Helper()
	base := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	allowance, err := domain.RestoreAllowance(domain.AllowanceSnapshot{Limit: 1000, Consumed: 100, Reserved: 100, Revision: 9})
	if err != nil {
		t.Fatal(err)
	}
	price := domain.PriceVersion{ID: "price_a", ToolVersionID: "tool_a", Currency: "USD", ReserveMicro: 100, ChargeMicro: 70, BillingPolicy: domain.FixedSuccessOnly, StartsAt: base.Add(-time.Hour), EndsAt: base.Add(time.Hour), Active: true}
	repo := &settlementRepoFake{locked: LockedSettlement{Price: price, Allowance: allowance, AllowanceRevision: 9, ReservationState: "held", ReservationAmount: 100, ReservationAt: base}}
	service, err := NewSettlementService(repo)
	if err != nil {
		t.Fatal(err)
	}
	q := SettlementRequest{WorkspaceID: "ws_a", RunID: "run_a", ReservationID: "res_run_a", PriceVersionID: "price_a", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD", ReservedMicro: 100, Outcome: outcome, ObservedAt: base.Add(time.Minute), SettledAt: base.Add(2 * time.Minute)}
	return service, repo, q
}

func TestSettlementChargesSuccessAndReleasesUnusedHeldQuota(t *testing.T) {
	service, repo, q := settlementFixture(t, domain.OutcomeSucceeded)
	receipt, err := service.Settle(context.Background(), q)
	if err != nil || receipt.ChargedMicro != 70 || repo.storeCalls != 1 || repo.lastCharge != 70 {
		t.Fatal(receipt, repo.storeCalls, repo.lastCharge, err)
	}
	if repo.lastState != (domain.AllowanceSnapshot{Limit: 1000, Consumed: 170, Reserved: 0, Revision: 10}) || repo.lastRev != 9 {
		t.Fatal(repo.lastState, repo.lastRev)
	}
}

func TestSettlementFailureAndCancellationConsumeZeroQuota(t *testing.T) {
	for _, outcome := range []domain.SettlementOutcome{domain.OutcomeFailed, domain.OutcomeCanceled} {
		service, repo, q := settlementFixture(t, outcome)
		receipt, err := service.Settle(context.Background(), q)
		if err != nil || receipt.ChargedMicro != 0 || repo.lastState != (domain.AllowanceSnapshot{Limit: 1000, Consumed: 100, Reserved: 0, Revision: 10}) {
			t.Fatal(outcome, receipt, repo.lastState, err)
		}
	}
}

func TestSettlementReplayMustMatchImmutableBusinessFact(t *testing.T) {
	service, repo, q := settlementFixture(t, domain.OutcomeSucceeded)
	repo.locked.Existing = &SettlementReceipt{WorkspaceID: q.WorkspaceID, RunID: q.RunID, ReservationID: q.ReservationID, PriceVersionID: q.PriceVersionID, BudgetID: q.BudgetID, PeriodID: q.PeriodID, Currency: q.Currency, ReservedMicro: q.ReservedMicro, ChargedMicro: 70, Outcome: q.Outcome, ObservedAt: q.ObservedAt, SettledAt: q.SettledAt.Add(-time.Second)}
	r, err := service.Settle(context.Background(), q)
	if err != nil || !r.Replay || repo.storeCalls != 0 {
		t.Fatal(r, repo.storeCalls, err)
	}
	conflict := q
	conflict.Outcome = domain.OutcomeFailed
	if _, err = service.Settle(context.Background(), conflict); !errors.Is(err, ErrSettlementConflict) {
		t.Fatal("conflicting replay accepted", err)
	}
}

func TestSettlementRejectsUnknownOutcomeAndPreReservationObservation(t *testing.T) {
	service, repo, q := settlementFixture(t, domain.SettlementOutcome("unknown"))
	if _, err := service.Settle(context.Background(), q); !errors.Is(err, ErrSettlementConflict) || repo.storeCalls != 0 {
		t.Fatal(err, repo.storeCalls)
	}
	service, repo, q = settlementFixture(t, domain.OutcomeSucceeded)
	q.ObservedAt = repo.locked.ReservationAt.Add(-time.Microsecond)
	if _, err := service.Settle(context.Background(), q); !errors.Is(err, ErrSettlementConflict) || repo.storeCalls != 0 {
		t.Fatal(err, repo.storeCalls)
	}
}
