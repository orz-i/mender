package domain

import (
	"testing"
	"time"
)

func TestFixedSuccessOnlyPriceSettlement(t *testing.T) {
	start := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	p := PriceVersion{ID: "price_a", ToolVersionID: "tool_a", Currency: "USD", ReserveMicro: 100, ChargeMicro: 70, BillingPolicy: FixedSuccessOnly, StartsAt: start, EndsAt: start.Add(time.Hour), Active: true}
	if !p.UsableAt(start.Add(time.Minute)) {
		t.Fatal("valid price rejected")
	}
	for outcome, want := range map[SettlementOutcome]int64{OutcomeSucceeded: 70, OutcomeFailed: 0, OutcomeCanceled: 0} {
		got, err := p.ChargeFor(outcome)
		if err != nil || got != want {
			t.Fatal(outcome, got, err)
		}
	}
	if _, err := p.ChargeFor("unknown"); err == nil {
		t.Fatal("unknown outcome accepted")
	}
	p.ChargeMicro = 101
	if p.ValidContract() {
		t.Fatal("charge above reservation accepted")
	}
}
