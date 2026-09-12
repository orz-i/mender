// Package application coordinates admission through consumer-owned ports, never SQL or SDKs.
package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid           = errors.New("invalid admission request or plan")
	ErrUnauthenticated   = errors.New("admission requires authentication")
	ErrForbidden         = errors.New("admission forbidden")
	ErrConflict          = errors.New("idempotency key reused with different request")
	ErrBudgetExceeded    = errors.New("admission allowance exceeded")
	ErrBudgetUnavailable = errors.New("admission allowance unavailable")
	ErrUnavailable       = errors.New("admission dependency unavailable")
	ErrCommitUnconfirmed = errors.New("admission commit unconfirmed; retry the same idempotency key")
)

type StartConstraint struct {
	ToolsetVersionID, ToolID, ToolVersion, ToolVersionID, ConnectionID, Currency string
	MaxChargeMicro                                                               int64
	IdempotencyKey                                                               string
}

type Caller struct {
	WorkspaceID, SubjectID, CredentialID string
	Start                                *StartConstraint
}
type Request struct {
	IdempotencyKey, ToolID, ToolVersion, ToolsetVersionID, ConnectionID, Currency, MaxChargeMicro string
	Arguments                                                                                     []byte
}

func validVersion(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for i, c := range s {
		if i == 0 && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

// Resolver must supply a published, immutable, authorized plan with a reliable cost cap.
// Public HTTP wiring remains outside this use case and is explicitly feature-gated by bootstrap.
type Plan struct {
	ToolID, ToolVersion, ToolVersionID, ToolsetVersionID, ConnectionID, PriceVersionID, DeploymentRevision string
	BudgetID, PeriodID, Currency                                                                           string
	ReserveMicro                                                                                           int64
	ValidUntil                                                                                             time.Time
}
type Prepared struct{ CanonicalArguments, RequestHash string }
type Record struct {
	WorkspaceID, SubjectID, CredentialID, IdempotencyKey, RequestHash, RunID, ReservationID string
	ToolVersionID, ToolsetVersionID, ConnectionID, PriceVersionID, DeploymentRevision       string
	BudgetID, PeriodID, Currency, CanonicalArguments                                        string
	ReservedMicro                                                                           int64
	CreatedAt                                                                               time.Time
}
type Receipt struct {
	WorkspaceID, RunID, ReservationID, Currency string
	ReservedMicro                               int64
	Replayed                                    bool
}
type Authorizer interface {
	Authorize(context.Context, Caller, Request) error
}
type Authenticator interface {
	Authenticate(context.Context, string) (Caller, error)
}
type Resolver interface {
	Resolve(context.Context, Caller, Request, string) (Plan, error)
}
type Codec interface {
	Prepare(Request) (Prepared, error)
}
type IDs interface{ NewRunID() (string, error) }
type Clock interface{ Now() time.Time }
type Scope interface {
	FindReplay(context.Context, string, string) (Record, bool, error)
	Reserve(context.Context, Record) error
	CreateRun(context.Context, Record) error
	CreateJob(context.Context, Record) error
	AppendOutbox(context.Context, Record) error
}
type UnitOfWork interface {
	Within(context.Context, string, func(Scope) error) error
}
type Service struct {
	auth     Authorizer
	resolver Resolver
	codec    Codec
	ids      IDs
	clock    Clock
	uow      UnitOfWork
}

func New(auth Authorizer, resolver Resolver, codec Codec, ids IDs, clock Clock, uow UnitOfWork) (*Service, error) {
	if auth == nil || resolver == nil || codec == nil || ids == nil || clock == nil || uow == nil {
		return nil, ErrUnavailable
	}
	return &Service{auth, resolver, codec, ids, clock, uow}, nil
}
func ValidID(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func validKey(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func amount(s string) (int64, error) {
	if s == "" || len(s) > 19 || len(s) > 1 && s[0] == '0' {
		return 0, ErrInvalid
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
	}
	v, e := strconv.ParseInt(s, 10, 64)
	if e != nil {
		return 0, ErrInvalid
	}
	return v, nil
}
func receipt(r Record, replayed bool) Receipt {
	return Receipt{r.WorkspaceID, r.RunID, r.ReservationID, r.Currency, r.ReservedMicro, replayed}
}
func (s *Service) Admit(ctx context.Context, c Caller, q Request) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	for _, id := range []string{c.WorkspaceID, c.SubjectID, c.CredentialID, q.ToolID, q.ToolsetVersionID, q.ConnectionID} {
		if !ValidID(id) {
			return Receipt{}, ErrInvalid
		}
	}
	cap, err := amount(q.MaxChargeMicro)
	if err != nil || !validKey(q.IdempotencyKey) || !validVersion(q.ToolVersion) || len(q.Currency) != 3 || strings.Trim(q.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return Receipt{}, ErrInvalid
	}
	if c.Start != nil {
		if !ValidID(c.Start.ToolsetVersionID) || !ValidID(c.Start.ToolID) || !validVersion(c.Start.ToolVersion) || !ValidID(c.Start.ToolVersionID) || !ValidID(c.Start.ConnectionID) || len(c.Start.Currency) != 3 || strings.Trim(c.Start.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" || c.Start.MaxChargeMicro < 0 || !validKey(c.Start.IdempotencyKey) {
			return Receipt{}, ErrInvalid
		}
	}
	// Every attempt, including replays, requires current authorization before any durable lookup.
	if err = s.auth.Authorize(ctx, c, q); err != nil {
		return Receipt{}, err
	}
	p, err := s.codec.Prepare(q)
	if err != nil {
		return Receipt{}, err
	}
	if len(p.RequestHash) != 64 || len(p.CanonicalArguments) == 0 || len(p.CanonicalArguments) > 65536 {
		return Receipt{}, ErrInvalid
	}
	plan, err := s.resolver.Resolve(ctx, c, q, p.CanonicalArguments)
	if err != nil {
		return Receipt{}, err
	}
	if plan.ToolID != q.ToolID || plan.ToolVersion != q.ToolVersion || !ValidID(plan.ToolVersionID) || plan.ToolsetVersionID != q.ToolsetVersionID || plan.ConnectionID != q.ConnectionID || plan.Currency != q.Currency || !ValidID(plan.PriceVersionID) || !ValidID(plan.DeploymentRevision) || !ValidID(plan.BudgetID) || !ValidID(plan.PeriodID) || plan.ReserveMicro < 0 || plan.ReserveMicro > cap {
		return Receipt{}, ErrInvalid
	}
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() || now.Year() < 1 || now.Year() > 9999 || !now.Before(plan.ValidUntil) {
		return Receipt{}, ErrInvalid
	}
	runID, err := s.ids.NewRunID()
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	if !ValidID(runID) || len(runID) > 120 {
		return Receipt{}, ErrInvalid
	}
	r := Record{WorkspaceID: c.WorkspaceID, SubjectID: c.SubjectID, CredentialID: c.CredentialID, IdempotencyKey: q.IdempotencyKey, RequestHash: p.RequestHash, RunID: runID, ReservationID: "res_" + runID, ToolVersionID: plan.ToolVersionID, ToolsetVersionID: plan.ToolsetVersionID, ConnectionID: plan.ConnectionID, PriceVersionID: plan.PriceVersionID, DeploymentRevision: plan.DeploymentRevision, BudgetID: plan.BudgetID, PeriodID: plan.PeriodID, Currency: q.Currency, CanonicalArguments: p.CanonicalArguments, ReservedMicro: plan.ReserveMicro, CreatedAt: now}
	var result Receipt
	err = s.uow.Within(ctx, c.WorkspaceID, func(tx Scope) error {
		old, found, e := tx.FindReplay(ctx, c.SubjectID, q.IdempotencyKey)
		if e != nil {
			return e
		}
		if found {
			if old.WorkspaceID != c.WorkspaceID || old.SubjectID != c.SubjectID || old.IdempotencyKey != q.IdempotencyKey {
				return ErrUnavailable
			}
			if old.RequestHash != p.RequestHash {
				return ErrConflict
			}
			if !ValidID(old.RunID) || !ValidID(old.ReservationID) || !ValidID(old.ToolVersionID) || !ValidID(old.ToolsetVersionID) || !ValidID(old.ConnectionID) || !ValidID(old.BudgetID) || !ValidID(old.PeriodID) || old.ToolsetVersionID != q.ToolsetVersionID || old.ConnectionID != q.ConnectionID || old.Currency != q.Currency || old.ReservedMicro < 0 || old.ReservedMicro > cap || old.CreatedAt.IsZero() {
				return ErrUnavailable
			}
			result = receipt(old, true)
			return nil
		}
		// Time is rechecked after the key lock; quota period validity is also checked under its row lock.
		r.CreatedAt = s.clock.Now().UTC().Truncate(time.Microsecond)
		if r.CreatedAt.Before(now) || !r.CreatedAt.Before(plan.ValidUntil) {
			return ErrInvalid
		}
		if e = tx.Reserve(ctx, r); e != nil {
			return e
		}
		if !s.clock.Now().Before(plan.ValidUntil) {
			return ErrInvalid
		}
		if e = tx.CreateRun(ctx, r); e != nil {
			return e
		}
		if e = tx.CreateJob(ctx, r); e != nil {
			return e
		}
		if e = tx.AppendOutbox(ctx, r); e != nil {
			return e
		}
		if !s.clock.Now().Before(plan.ValidUntil) {
			return ErrInvalid
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		result = receipt(r, false)
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	return result, nil
}
