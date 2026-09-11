package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type processClock struct{ at time.Time }

func (c processClock) Now() time.Time { return c.at }

type processScope struct {
	candidate                      Candidate
	found                          bool
	claimErr, settleErr, finishErr error
	settleCalls, finishCalls       int
	settledAt, finishedAt          time.Time
}

func (s *processScope) Claim(context.Context) (Candidate, bool, error) {
	return s.candidate, s.found, s.claimErr
}
func (s *processScope) Settle(_ context.Context, _ Candidate, at time.Time) (Receipt, error) {
	s.settleCalls++
	s.settledAt = at
	return Receipt{ChargedMicro: 70}, s.settleErr
}
func (s *processScope) Finish(_ context.Context, _ Candidate, at time.Time) error {
	s.finishCalls++
	s.finishedAt = at
	return s.finishErr
}

type processUoW struct {
	scope Scope
	err   error
	calls int
}

func (u *processUoW) Within(ctx context.Context, _ string, fn func(Scope) error) error {
	u.calls++
	if u.err != nil {
		return u.err
	}
	return fn(u.scope)
}

func processCandidate(base time.Time) Candidate {
	return Candidate{WorkspaceID: "ws_a", RunID: "run_a", ObservationID: "obs.a", ReservationID: "res_run_a", PriceVersionID: "price_a", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD", ReservedMicro: 100, Outcome: "succeeded", ObservedAt: base.Add(-time.Second)}
}

func TestSettlementProcessClaimsSettlesAndFinishesAtomically(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	scope := &processScope{candidate: processCandidate(now), found: true}
	uow := &processUoW{scope: scope}
	service, err := New(uow, processClock{now})
	if err != nil {
		t.Fatal(err)
	}
	r, err := service.SettleOne(context.Background(), "ws_a")
	if err != nil || r.ChargedMicro != 70 || uow.calls != 1 || scope.settleCalls != 1 || scope.finishCalls != 1 || !scope.settledAt.Equal(now) || !scope.finishedAt.Equal(now) {
		t.Fatal(r, uow.calls, scope, err)
	}
}

func TestSettlementProcessNoWorkAndFailuresDoNotFinish(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		scope *processScope
		want  error
	}{
		{"no work", &processScope{}, ErrNoWork},
		{"invalid candidate", &processScope{candidate: Candidate{WorkspaceID: "ws_a"}, found: true}, ErrInvalid},
		{"settlement failed", &processScope{candidate: processCandidate(now), found: true, settleErr: ErrUnavailable}, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uow := &processUoW{scope: tc.scope}
			service, _ := New(uow, processClock{now})
			_, err := service.SettleOne(context.Background(), "ws_a")
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if tc.scope.finishCalls != 0 {
				t.Fatal("failed work was marked finished")
			}
		})
	}
}

func TestSettlementProcessRejectsObservationFromFuture(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	c := processCandidate(now)
	c.ObservedAt = now.Add(time.Microsecond)
	scope := &processScope{candidate: c, found: true}
	service, _ := New(&processUoW{scope: scope}, processClock{now})
	_, err := service.SettleOne(context.Background(), "ws_a")
	if !errors.Is(err, ErrInvalid) || scope.settleCalls != 0 || scope.finishCalls != 0 {
		t.Fatal(err, scope)
	}
}
