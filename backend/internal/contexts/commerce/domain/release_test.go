package domain

import (
	"math"
	"testing"
)

func TestAllowanceReleaseOnlyReducesHeldQuota(t *testing.T) {
	a, e := RestoreAllowance(AllowanceSnapshot{Limit: 100, Consumed: 20, Reserved: 60, Revision: 1})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Release(60); e != nil {
		t.Fatal(e)
	}
	if s := a.Snapshot(); s != (AllowanceSnapshot{Limit: 100, Consumed: 20, Reserved: 0, Revision: 2}) {
		t.Fatal(s)
	}
	before := a.Snapshot()
	if e = a.Release(60); e == nil || a.Snapshot() != before {
		t.Fatal("duplicate release changed quota", e, a.Snapshot())
	}
	if e = a.Reserve(80); e != nil {
		t.Fatal("released capacity not reusable", e)
	}
}
func TestAllowanceReleaseRejectsUnderflowAndExhaustion(t *testing.T) {
	for _, tc := range []struct{ amount, reserved, revision int64 }{{-1, 1, 1}, {2, 1, 1}, {0, 0, math.MaxInt64}} {
		a, e := RestoreAllowance(AllowanceSnapshot{Limit: 100, Reserved: tc.reserved, Revision: tc.revision})
		if e != nil {
			t.Fatal(e)
		}
		before := a.Snapshot()
		if e = a.Release(tc.amount); e == nil || a.Snapshot() != before {
			t.Fatal(tc, e)
		}
	}
	var zero Allowance
	if e := zero.Release(0); e == nil {
		t.Fatal("uninitialized quota accepted")
	}
	a, _ := RestoreAllowance(AllowanceSnapshot{Limit: 0, Revision: 1})
	if e := a.Release(0); e != nil || a.Snapshot().Revision != 2 {
		t.Fatal("zero-cost release", e)
	}
}
