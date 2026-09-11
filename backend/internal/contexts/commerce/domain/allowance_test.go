package domain

import (
	"errors"
	"math"
	"testing"
)

func TestExactMicroAndQuotaBoundaries(t *testing.T) {
	for _, s := range []string{"", "-1", "+1", "01", "1.0", "1e3", " 1", "9223372036854775808"} {
		if _, e := ParseMicro(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"0", "1", "9007199254740993", "9223372036854775807"} {
		if _, e := ParseMicro(s); e != nil {
			t.Fatal(s, e)
		}
	}
	a, e := RestoreAllowance(AllowanceSnapshot{Limit: math.MaxInt64, Revision: 1})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Reserve(math.MaxInt64); e != nil {
		t.Fatal(e)
	}
	before := a.Snapshot()
	if e = a.Reserve(1); !errors.Is(e, ErrLimitExceeded) || a.Snapshot() != before {
		t.Fatal("overflow/overspend mutated quota", e)
	}
	for _, s := range []AllowanceSnapshot{{Limit: 1, Consumed: 2, Revision: 1}, {Limit: 1, Reserved: 2, Revision: 1}, {Limit: 1, Reserved: -1, Revision: 1}, {Limit: 1}} {
		if _, e := RestoreAllowance(s); e == nil {
			t.Fatal("invalid snapshot", s)
		}
	}
	a, _ = RestoreAllowance(AllowanceSnapshot{Limit: 10, Revision: math.MaxInt64})
	before = a.Snapshot()
	if e = a.Reserve(0); e == nil || a.Snapshot() != before {
		t.Fatal("revision overflow")
	}
}

func TestAllowanceSettlementConvertsHeldQuotaToConsumption(t *testing.T) {
	a, err := RestoreAllowance(AllowanceSnapshot{Limit: 100, Consumed: 10, Reserved: 40, Revision: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Settle(40, 25); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot(); got != (AllowanceSnapshot{Limit: 100, Consumed: 35, Reserved: 0, Revision: 8}) {
		t.Fatal(got)
	}
	for _, tc := range [][2]int64{{1, 2}, {41, 1}, {-1, 0}, {1, -1}} {
		if err = a.Settle(tc[0], tc[1]); err == nil {
			t.Fatal("invalid settlement accepted", tc)
		}
	}
}
