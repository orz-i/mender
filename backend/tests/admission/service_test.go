package admission_test

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/input"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"strings"
	"testing"
	"time"
)

var moment = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
var who = application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}

func request() application.Request {
	return application.Request{IdempotencyKey: "request_0001", ToolVersionID: "tool_v1", ToolsetVersionID: "set_v1", ConnectionID: "conn_a", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD", MaxChargeMicro: "100", Arguments: []byte(`{"n":9007199254740993,"text":"data"}`)}
}
func plan(q application.Request) application.Plan {
	return application.Plan{ToolVersionID: q.ToolVersionID, ToolsetVersionID: q.ToolsetVersionID, ConnectionID: q.ConnectionID, PriceVersionID: "price_v1", DeploymentRevision: "deploy_v1", Currency: q.Currency, ReserveMicro: 50, ValidUntil: moment.Add(time.Hour)}
}

type authFunc func(context.Context, application.Caller, application.Request) error

func (f authFunc) Authorize(c context.Context, w application.Caller, q application.Request) error {
	return f(c, w, q)
}

type resolverFunc func(context.Context, application.Caller, application.Request, string) (application.Plan, error)

func (f resolverFunc) Resolve(c context.Context, w application.Caller, q application.Request, a string) (application.Plan, error) {
	return f(c, w, q, a)
}

type clock struct{ at time.Time }

func (c clock) Now() time.Time { return c.at }

type ids struct{}

func (ids) NewRunID() (string, error) { return "run_test", nil }

type unit struct {
	calls     int
	scope     *scope
	commitErr error
}

func (u *unit) Within(ctx context.Context, w string, fn func(application.Scope) error) error {
	u.calls++
	if e := fn(u.scope); e != nil {
		return e
	}
	return u.commitErr
}

// This fake only verifies use-case order; database atomicity has separate real PostgreSQL tests.
type scope struct {
	old      application.Record
	found    bool
	ops      []string
	fail     string
	captured application.Record
}

func (s *scope) FindReplay(context.Context, string, string) (application.Record, bool, error) {
	s.ops = append(s.ops, "replay")
	return s.old, s.found, nil
}
func (s *scope) step(name string, r application.Record) error {
	s.ops = append(s.ops, name)
	s.captured = r
	if name == s.fail {
		return application.ErrUnavailable
	}
	return nil
}
func (s *scope) Reserve(_ context.Context, r application.Record) error   { return s.step("reserve", r) }
func (s *scope) CreateRun(_ context.Context, r application.Record) error { return s.step("run", r) }
func (s *scope) CreateJob(_ context.Context, r application.Record) error { return s.step("job", r) }
func (s *scope) AppendOutbox(_ context.Context, r application.Record) error {
	return s.step("outbox", r)
}
func build(t *testing.T, u *unit, auth authFunc, resolve resolverFunc) *application.Service {
	t.Helper()
	s, e := application.New(auth, resolve, input.Codec{}, ids{}, clock{moment}, u)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func allow(context.Context, application.Caller, application.Request) error { return nil }
func resolve(_ context.Context, _ application.Caller, q application.Request, _ string) (application.Plan, error) {
	return plan(q), nil
}

func TestAdmissionOrderStableRequestAndNoFalseSuccess(t *testing.T) {
	u := &unit{scope: &scope{}}
	s := build(t, u, allow, resolve)
	r, e := s.Admit(context.Background(), who, request())
	if e != nil || r.Replayed || r.ReservedMicro != 50 {
		t.Fatal(r, e)
	}
	if strings.Join(u.scope.ops, ",") != "replay,reserve,run,job,outbox" {
		t.Fatal(u.scope.ops)
	}
	if !strings.Contains(u.scope.captured.CanonicalArguments, "9007199254740993") {
		t.Fatal("integer precision lost")
	}
	u.scope = &scope{old: u.scope.captured, found: true}
	r, e = s.Admit(context.Background(), who, request())
	if e != nil || !r.Replayed || len(u.scope.ops) != 1 {
		t.Fatal("replay mutated", r, e)
	}
	q := request()
	q.MaxChargeMicro = "101"
	if _, e = s.Admit(context.Background(), who, q); !errors.Is(e, application.ErrConflict) {
		t.Fatal(e)
	}
	for _, step := range []string{"reserve", "run", "job", "outbox"} {
		u.scope = &scope{fail: step}
		r, e = s.Admit(context.Background(), who, request())
		if e == nil || r.RunID != "" {
			t.Fatal("failed write reported accepted", step, r, e)
		}
	}
	u.scope = &scope{}
	u.commitErr = application.ErrCommitUnconfirmed
	if r, e = s.Admit(context.Background(), who, request()); !errors.Is(e, application.ErrCommitUnconfirmed) || r.RunID != "" {
		t.Fatal("uncertain commit reported success", r, e)
	}
}
func TestDeniedRevokedAndInvalidPlansNeverEnterTransaction(t *testing.T) {
	u := &unit{scope: &scope{found: true}}
	s := build(t, u, func(context.Context, application.Caller, application.Request) error { return application.ErrForbidden }, resolve)
	if _, e := s.Admit(context.Background(), who, request()); !errors.Is(e, application.ErrForbidden) || u.calls != 0 {
		t.Fatal(e)
	}
	for _, modify := range []func(*application.Plan){func(p *application.Plan) { p.ToolVersionID = "other" }, func(p *application.Plan) { p.Currency = "EUR" }, func(p *application.Plan) { p.ReserveMicro = 101 }, func(p *application.Plan) { p.ReserveMicro = -1 }, func(p *application.Plan) { p.ValidUntil = moment }, func(p *application.Plan) { p.PriceVersionID = "" }} {
		u = &unit{scope: &scope{}}
		s = build(t, u, allow, func(c context.Context, w application.Caller, q application.Request, a string) (application.Plan, error) {
			p := plan(q)
			modify(&p)
			return p, nil
		})
		if _, e := s.Admit(context.Background(), who, request()); e == nil || u.calls != 0 {
			t.Fatal("untrusted plan entered transaction")
		}
	}
	u = &unit{scope: &scope{}}
	s = build(t, u, allow, resolve)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.Admit(ctx, who, request()); !errors.Is(e, context.Canceled) || u.calls != 0 {
		t.Fatal(e)
	}
}
func TestCanonicalJSONRejectsAmbiguityAndBindsEveryRequestField(t *testing.T) {
	c := input.Codec{}
	q := request()
	a, e := c.Prepare(q)
	if e != nil {
		t.Fatal(e)
	}
	q.Arguments = []byte(" { \"text\":\"data\", \"n\" : 9007199254740993 } ")
	b, e := c.Prepare(q)
	if e != nil || a != b {
		t.Fatal("key order/whitespace changed identity", e)
	}
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"n":1,"n":2}`, `{"x":{"a":1,"\u0061":2}}`, strings.Repeat("[", 34) + strings.Repeat("]", 34), strings.Repeat(" ", 65537)} {
		q.Arguments = []byte(raw)
		if _, e = c.Prepare(q); e == nil {
			t.Fatalf("accepted ambiguous input %q", raw[:min(len(raw), 80)])
		}
	}
	mutations := []func(*application.Request){func(q *application.Request) { q.ToolVersionID = "tool_v2" }, func(q *application.Request) { q.ToolsetVersionID = "set_v2" }, func(q *application.Request) { q.ConnectionID = "conn_b" }, func(q *application.Request) { q.BudgetID = "budget_b" }, func(q *application.Request) { q.PeriodID = "period_b" }, func(q *application.Request) { q.Currency = "EUR" }, func(q *application.Request) { q.MaxChargeMicro = "101" }, func(q *application.Request) { q.Arguments = []byte(`{"n":9007199254740992,"text":"data"}`) }}
	for _, change := range mutations {
		q = request()
		change(&q)
		b, e = c.Prepare(q)
		if e != nil || a.RequestHash == b.RequestHash {
			t.Fatal("request field not bound", q, e)
		}
	}
}
func TestNoImplicitAuthorizationOrResolverIsInstalled(t *testing.T) {
	for _, deps := range []struct {
		a application.Authorizer
		r application.Resolver
	}{{nil, resolverFunc(resolve)}, {authFunc(allow), nil}} {
		if _, e := application.New(deps.a, deps.r, input.Codec{}, ids{}, clock{moment}, &unit{}); e == nil {
			t.Fatal("missing authoritative port allowed")
		}
	}
}
