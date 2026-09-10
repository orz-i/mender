package execution_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type queryStore struct {
	runs   func(context.Context, ports.WorkspaceID, ports.RunFilter) ([]domain.Snapshot, error)
	events func(context.Context, ports.WorkspaceID, ports.RunID, ports.EventFilter) (ports.EventBatch, error)
}

func (s queryStore) FetchRuns(c context.Context, w ports.WorkspaceID, f ports.RunFilter) ([]domain.Snapshot, error) {
	return s.runs(c, w, f)
}
func (s queryStore) FetchEvents(c context.Context, w ports.WorkspaceID, id ports.RunID, f ports.EventFilter) (ports.EventBatch, error) {
	return s.events(c, w, id, f)
}

type queryClock struct{ now time.Time }

func (c *queryClock) Now() time.Time { return c.now }

var queryCaller = ports.Caller{WorkspaceID: "ws_a", SubjectID: "subject_a", CredentialID: "key_a"}

func queryCodec(t *testing.T) *cursor.Codec {
	t.Helper()
	c, e := cursor.New([]byte(strings.Repeat("q", 32)))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func queryService(t *testing.T, s ports.ReadRepository, a ports.Authorizer, c ports.Clock) *application.Queries {
	t.Helper()
	q, e := application.NewQueries(s, a, queryCodec(t), c)
	if e != nil {
		t.Fatal(e)
	}
	return q
}
func runRow(t *testing.T, id, workspace string) domain.Snapshot {
	t.Helper()
	r, e := domain.NewQueuedRun(domain.RunID(id), domain.WorkspaceID(workspace), at)
	if e != nil {
		t.Fatal(e)
	}
	return r.Snapshot()
}
func orderedRows(rows []domain.Snapshot) queryStore {
	return queryStore{runs: func(_ context.Context, w ports.WorkspaceID, f ports.RunFilter) ([]domain.Snapshot, error) {
		result := []domain.Snapshot{}
		for _, r := range rows {
			if r.WorkspaceID != w || f.State != "" && r.State != f.State {
				continue
			}
			if !f.BeforeCreated.IsZero() && (r.CreatedAt.After(f.BeforeCreated) || r.CreatedAt.Equal(f.BeforeCreated) && r.ID >= f.BeforeID) {
				continue
			}
			result = append(result, r)
		}
		slices.SortFunc(result, func(a, b domain.Snapshot) int {
			if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
				return c
			}
			return strings.Compare(string(b.ID), string(a.ID))
		})
		return result[:min(len(result), f.Size+1)], nil
	}}
}

func TestRunKeysetPaginationUsesImmutableCompositeKeyAndLiveFilter(t *testing.T) {
	clock := &queryClock{at.Add(time.Minute)}
	rows := []domain.Snapshot{runRow(t, "run_z", "ws_a"), runRow(t, "run_a", "ws_a"), runRow(t, "run_A", "ws_a"), runRow(t, "run_hidden", "ws_b")}
	var actions int
	policy := authorizeFunc(func(_ context.Context, c ports.Caller, a ports.Action, id domain.RunID) error {
		actions++
		if c != queryCaller || a != ports.ListRuns || id != "" {
			t.Fatal("wrong collection authorization")
		}
		return nil
	})
	q := queryService(t, orderedRows(rows), policy, clock)
	page, e := q.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Limit: 1}, State: "queued"})
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != "run_z" || page.NextCursor == "" {
		t.Fatal(page, e)
	}
	first, e := queryCodec(t).Decode(page.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	clock.now = clock.now.Add(time.Minute)
	page, e = q.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Limit: 1, Cursor: page.NextCursor}, State: "queued"})
	if e != nil || page.Items[0].ID != "run_a" {
		t.Fatal(page, e)
	}
	second, e := queryCodec(t).Decode(page.NextCursor)
	if e != nil || !first.ExpiresAt.Equal(second.ExpiresAt) {
		t.Fatal("pagination must not extend cursor expiry", e)
	}
	page, e = q.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Limit: 1, Cursor: page.NextCursor}, State: "queued"})
	if e != nil || page.Items[0].ID != "run_A" || page.NextCursor != "" || actions != 3 {
		t.Fatal(page, e, actions)
	}
	page, e = q.ListRuns(context.Background(), queryCaller, application.RunListRequest{State: "running"})
	if e != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("empty response must be an array", e)
	}
}

func TestSignedCursorBindsIdentityFilterSizeEndpointAndExpiry(t *testing.T) {
	clock := &queryClock{at.Add(time.Minute)}
	q := queryService(t, orderedRows([]domain.Snapshot{runRow(t, "run_b", "ws_a"), runRow(t, "run_a", "ws_a")}), allow(), clock)
	p, e := q.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Limit: 1}})
	if e != nil {
		t.Fatal(e)
	}
	base := application.RunListRequest{PageRequest: application.PageRequest{Limit: 1, Cursor: p.NextCursor}}
	noStorage := queryService(t, queryStore{}, authorizeFunc(func(context.Context, ports.Caller, ports.Action, domain.RunID) error { return nil }), clock)
	for name, mutate := range map[string]func(*ports.Caller, *application.RunListRequest){
		"workspace":  func(c *ports.Caller, _ *application.RunListRequest) { c.WorkspaceID = "ws_b" },
		"subject":    func(c *ports.Caller, _ *application.RunListRequest) { c.SubjectID = "another" },
		"credential": func(c *ports.Caller, _ *application.RunListRequest) { c.CredentialID = "other_key" },
		"state":      func(_ *ports.Caller, r *application.RunListRequest) { r.State = "queued" },
		"size":       func(_ *ports.Caller, r *application.RunListRequest) { r.Limit = 2 },
		"signature":  func(_ *ports.Caller, r *application.RunListRequest) { r.Cursor += "x" },
	} {
		t.Run(name, func(t *testing.T) {
			c, r := queryCaller, base
			mutate(&c, &r)
			if _, e := noStorage.ListRuns(context.Background(), c, r); !errors.Is(e, application.ErrInvalidCursor) {
				t.Fatal(e)
			}
		})
	}
	if _, e = noStorage.ListEvents(context.Background(), queryCaller, "run_a", base.PageRequest); !errors.Is(e, application.ErrInvalidCursor) {
		t.Fatal("collection cursor used for events", e)
	}
	clock.now = clock.now.Add(application.CursorLifetime)
	if _, e = noStorage.ListRuns(context.Background(), queryCaller, base); !errors.Is(e, application.ErrInvalidCursor) {
		t.Fatal("expiry boundary", e)
	}
}

func TestQueriesAuthorizeBeforeCursorOrStorageAndPropagateCancellation(t *testing.T) {
	clock := &queryClock{at}
	denied := queryService(t, queryStore{}, authorizeFunc(func(context.Context, ports.Caller, ports.Action, domain.RunID) error { return ports.ErrForbidden }), clock)
	if _, e := denied.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Cursor: "invalid"}}); !errors.Is(e, ports.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := denied.ListEvents(context.Background(), queryCaller, "run_a", application.PageRequest{}); !errors.Is(e, ports.ErrForbidden) {
		t.Fatal(e)
	}
	for _, c := range []ports.Caller{{}, {WorkspaceID: "ws_a", SubjectID: "subject_a"}} {
		if _, e := denied.ListRuns(context.Background(), c, application.RunListRequest{}); !errors.Is(e, application.ErrInvalidRequest) {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := denied.ListRuns(ctx, queryCaller, application.RunListRequest{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	q := queryService(t, queryStore{}, allow(), clock)
	for _, size := range []int{-1, 101} {
		if _, e := q.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Limit: size}}); !errors.Is(e, application.ErrInvalidRequest) {
			t.Fatal(e)
		}
	}
	if _, e := q.ListRuns(context.Background(), queryCaller, application.RunListRequest{State: "QUEUED"}); !errors.Is(e, application.ErrInvalidRequest) {
		t.Fatal(e)
	}
	if _, e := q.ListEvents(context.Background(), queryCaller, "../run", application.PageRequest{}); !errors.Is(e, application.ErrInvalidRequest) {
		t.Fatal(e)
	}
	q = queryService(t, queryStore{runs: func(context.Context, ports.WorkspaceID, ports.RunFilter) ([]domain.Snapshot, error) {
		return nil, ports.ErrUnavailable
	}}, allow(), clock)
	if _, e := q.ListRuns(context.Background(), queryCaller, application.RunListRequest{}); !errors.Is(e, ports.ErrUnavailable) {
		t.Fatal(e)
	}
}

func TestMalformedReadAdaptersCannotLeakDataOrBreakOrdering(t *testing.T) {
	clock := &queryClock{at}
	a, b := runRow(t, "run_a", "ws_a"), runRow(t, "run_b", "ws_a")
	bad := a
	bad.State = "garbage"
	for name, rows := range map[string][]domain.Snapshot{"other tenant": {runRow(t, "run_a", "ws_b")}, "duplicate": {a, a}, "reversed": {a, b}, "corrupt": {bad}, "too many": {b, a, a}} {
		t.Run(name, func(t *testing.T) {
			q := queryService(t, queryStore{runs: func(context.Context, ports.WorkspaceID, ports.RunFilter) ([]domain.Snapshot, error) { return rows, nil }}, allow(), clock)
			if _, e := q.ListRuns(context.Background(), queryCaller, application.RunListRequest{PageRequest: application.PageRequest{Limit: 1}}); !errors.Is(e, ports.ErrUnavailable) {
				t.Fatal(e)
			}
		})
	}
}

func event(version uint64) ports.Event {
	return ports.Event{WorkspaceID: "ws_a", RunID: "run_a", Version: version, State: domain.Running, OccurredAt: at, SubjectID: "subject_a", Reason: "audited change"}
}

func TestEventPaginationPinsUpperRevisionWithoutClaimingSSETailing(t *testing.T) {
	clock := &queryClock{at.Add(time.Minute)}
	events := []ports.Event{event(2), event(3), event(4)}
	store := queryStore{events: func(_ context.Context, w ports.WorkspaceID, id ports.RunID, f ports.EventFilter) (ports.EventBatch, error) {
		if w != "ws_a" || id != "run_a" {
			return ports.EventBatch{}, ports.ErrNotFound
		}
		upper := f.Through
		if upper == 0 {
			upper = events[len(events)-1].Version
		}
		result := ports.EventBatch{Through: upper, Items: []ports.Event{}}
		for _, e := range events {
			if e.Version > f.After && e.Version <= upper {
				result.Items = append(result.Items, e)
			}
		}
		result.Items = result.Items[:min(len(result.Items), f.Size+1)]
		return result, nil
	}}
	q := queryService(t, store, allow(), clock)
	p, e := q.ListEvents(context.Background(), queryCaller, "run_a", application.PageRequest{Limit: 1})
	if e != nil || p.ThroughVersion != 4 || p.Items[0].Version != 2 || p.NextCursor == "" {
		t.Fatal(p, e)
	}
	events = append(events, event(5))
	for _, want := range []uint64{3, 4} {
		p, e = q.ListEvents(context.Background(), queryCaller, "run_a", application.PageRequest{Limit: 1, Cursor: p.NextCursor})
		if e != nil || p.ThroughVersion != 4 || p.Items[0].Version != want {
			t.Fatal(p, e)
		}
	}
	if p.NextCursor != "" {
		t.Fatal("new event escaped pinned watermark")
	}
	p, e = q.ListEvents(context.Background(), queryCaller, "run_a", application.PageRequest{})
	if e != nil || p.ThroughVersion != 5 || len(p.Items) != 4 {
		t.Fatal(p, e)
	}
	if _, e = q.ListEvents(context.Background(), queryCaller, "missing", application.PageRequest{}); !errors.Is(e, ports.ErrNotFound) {
		t.Fatal(e)
	}
}

func TestEventsDistinguishEmptyExistingRunAndRejectCorruptProjection(t *testing.T) {
	for name, batch := range map[string]ports.EventBatch{
		"zero watermark": {}, "out of order": {Through: 4, Items: []ports.Event{event(3), event(2)}}, "above watermark": {Through: 2, Items: []ports.Event{event(3)}},
		"wrong run": {Through: 3, Items: []ports.Event{{WorkspaceID: "ws_a", RunID: "other", Version: 2, State: domain.Running, OccurredAt: at, SubjectID: "a"}}},
	} {
		t.Run(name, func(t *testing.T) {
			q := queryService(t, queryStore{events: func(context.Context, ports.WorkspaceID, ports.RunID, ports.EventFilter) (ports.EventBatch, error) {
				return batch, nil
			}}, allow(), fixedClock{at})
			if _, e := q.ListEvents(context.Background(), queryCaller, "run_a", application.PageRequest{}); !errors.Is(e, ports.ErrUnavailable) {
				t.Fatal(e)
			}
		})
	}
	q := queryService(t, queryStore{events: func(context.Context, ports.WorkspaceID, ports.RunID, ports.EventFilter) (ports.EventBatch, error) {
		return ports.EventBatch{Through: 1}, nil
	}}, allow(), fixedClock{at})
	p, e := q.ListEvents(context.Background(), queryCaller, "run_a", application.PageRequest{})
	if e != nil || p.Items == nil || len(p.Items) != 0 || p.ThroughVersion != 1 {
		t.Fatal(p, e)
	}
}
