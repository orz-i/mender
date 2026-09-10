package domain

import (
	"errors"
	"math"
	"strconv"
)

var (
	ErrInvalidAllowance = errors.New("invalid allowance or amount")
	ErrLimitExceeded    = errors.New("allowance limit exceeded")
)

// ParseMicro accepts canonical non-negative base-10 integers, never floats or exponents.
func ParseMicro(value string) (int64, error) {
	if value == "" || len(value) > 19 || len(value) > 1 && value[0] == '0' {
		return 0, ErrInvalidAllowance
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, ErrInvalidAllowance
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidAllowance
	}
	return n, nil
}

// Allowance controls admission quota, not a payment wallet, revenue or accounting ledger.
type Allowance struct{ limit, consumed, reserved, revision int64 }
type AllowanceSnapshot struct{ Limit, Consumed, Reserved, Revision int64 }

func RestoreAllowance(s AllowanceSnapshot) (Allowance, error) {
	if s.Limit < 0 || s.Consumed < 0 || s.Reserved < 0 || s.Revision < 1 || s.Consumed > s.Limit || s.Reserved > s.Limit-s.Consumed {
		return Allowance{}, ErrInvalidAllowance
	}
	return Allowance{limit: s.Limit, consumed: s.Consumed, reserved: s.Reserved, revision: s.Revision}, nil
}
func (a Allowance) Snapshot() AllowanceSnapshot {
	return AllowanceSnapshot{a.limit, a.consumed, a.reserved, a.revision}
}
func (a *Allowance) Reserve(amount int64) error {
	if a.revision < 1 || amount < 0 || a.revision == math.MaxInt64 {
		return ErrInvalidAllowance
	}
	if amount > a.limit-a.consumed-a.reserved {
		return ErrLimitExceeded
	}
	a.reserved += amount
	a.revision++
	return nil
}

// Release only relinquishes an existing reservation. It neither refunds a payment
// nor changes consumed quota. The owning service must prove reservation identity/state.
func (a *Allowance) Release(amount int64) error {
	if a.revision < 1 || amount < 0 || amount > a.reserved || a.revision == math.MaxInt64 {
		return ErrInvalidAllowance
	}
	a.reserved -= amount
	a.revision++
	return nil
}
