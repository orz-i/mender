package application

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var ErrInvalidCursor = errors.New("invalid or expired pagination cursor")

const CursorLifetime = 15 * time.Minute

type PageRequest struct {
	Limit  int // Zero uses 20. Explicit HTTP limit=0 is rejected by the transport.
	Cursor string
}
type RunListRequest struct {
	PageRequest
	State string
}
type RunPage struct {
	Items      []View
	NextCursor string
}
type EventPage struct {
	Items          []ports.Event
	NextCursor     string
	ThroughVersion uint64
}

type Queries struct {
	repository ports.ReadRepository
	authorizer ports.Authorizer
	codec      ports.CursorCodec
	clock      ports.Clock
}

func NewQueries(repo ports.ReadRepository, authorizer ports.Authorizer, codec ports.CursorCodec, clock ports.Clock) (*Queries, error) {
	if repo == nil || authorizer == nil || codec == nil || clock == nil {
		return nil, ErrMissingDependency
	}
	return &Queries{repo, authorizer, codec, clock}, nil
}

func validState(state string) bool {
	switch domain.State(state) {
	case domain.Queued, domain.Running, domain.WaitingInput, domain.CancelRequested, domain.Reconciling, domain.Succeeded, domain.Failed, domain.Canceled, domain.TimedOut:
		return true
	}
	return false
}

func (q *Queries) authorize(ctx context.Context, c ports.Caller, action ports.Action, id ports.RunID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.WorkspaceID.IsValid() || strings.TrimSpace(c.SubjectID) == "" || len(c.SubjectID) > 512 || strings.TrimSpace(c.CredentialID) == "" || len(c.CredentialID) > 128 {
		return ErrInvalidRequest
	}
	if err := q.authorizer.Authorize(ctx, c, action, id); err != nil {
		return err
	}
	return ctx.Err()
}

func (q *Queries) continuation(c ports.Caller, r PageRequest, kind, state string, id ports.RunID) (ports.Cursor, error) {
	size := r.Limit
	if size == 0 {
		size = 20
	}
	if size < 1 || size > 100 || len(r.Cursor) > 2048 {
		return ports.Cursor{}, ErrInvalidRequest
	}
	now := q.clock.Now().UTC()
	if now.IsZero() {
		return ports.Cursor{}, ports.ErrUnavailable
	}
	if r.Cursor == "" {
		return ports.Cursor{Format: 1, Kind: kind, Workspace: c.WorkspaceID, Subject: c.SubjectID, Credential: c.CredentialID, State: state, Size: size, Run: id, ExpiresAt: now.Add(CursorLifetime)}, nil
	}
	v, err := q.codec.Decode(r.Cursor)
	if err != nil || v.Format != 1 || v.Kind != kind || v.Workspace != c.WorkspaceID || v.Subject != c.SubjectID || v.Credential != c.CredentialID || v.State != state || v.Size != size || v.Run != id || !now.Before(v.ExpiresAt) || v.ExpiresAt.After(now.Add(CursorLifetime)) {
		return ports.Cursor{}, ErrInvalidCursor
	}
	if kind == "runs" {
		if v.BeforeCreated.IsZero() || v.BeforeCreated.Year() < 1 || v.BeforeCreated.Year() > 9999 || !v.BeforeCreated.Equal(v.BeforeCreated.Truncate(time.Microsecond)) || !v.BeforeID.IsValid() || v.After != 0 || v.Through != 0 {
			return ports.Cursor{}, ErrInvalidCursor
		}
	} else if !v.BeforeCreated.IsZero() || v.BeforeID != "" || v.After < 2 || v.Through > math.MaxInt64 || v.After >= v.Through {
		return ports.Cursor{}, ErrInvalidCursor
	}
	return v, nil
}

// ListRuns uses immutable (created_at, id) keys; state filters are live views,
// not a database snapshot retained across HTTP requests.
func (q *Queries) ListRuns(ctx context.Context, caller ports.Caller, request RunListRequest) (RunPage, error) {
	if request.State != "" && !validState(request.State) {
		return RunPage{}, ErrInvalidRequest
	}
	if err := q.authorize(ctx, caller, ports.ListRuns, ""); err != nil {
		return RunPage{}, err
	}
	c, err := q.continuation(caller, request.PageRequest, "runs", request.State, "")
	if err != nil {
		return RunPage{}, err
	}
	rows, err := q.repository.FetchRuns(ctx, caller.WorkspaceID, ports.RunFilter{State: domain.State(c.State), Size: c.Size, BeforeCreated: c.BeforeCreated, BeforeID: c.BeforeID})
	if err != nil {
		return RunPage{}, err
	}
	if err = ctx.Err(); err != nil {
		return RunPage{}, err
	}
	if len(rows) > c.Size+1 {
		return RunPage{}, ports.ErrUnavailable
	}
	previousTime, previousID := c.BeforeCreated, c.BeforeID
	for _, row := range rows {
		if _, err = domain.Restore(row); err != nil || row.WorkspaceID != caller.WorkspaceID || row.Version > math.MaxInt64 || !row.CreatedAt.Equal(row.CreatedAt.Truncate(time.Microsecond)) || (c.State != "" && string(row.State) != c.State) {
			return RunPage{}, ports.ErrUnavailable
		}
		if !previousTime.IsZero() && (row.CreatedAt.After(previousTime) || row.CreatedAt.Equal(previousTime) && strings.Compare(string(row.ID), string(previousID)) >= 0) {
			return RunPage{}, ports.ErrUnavailable
		}
		previousTime, previousID = row.CreatedAt, row.ID
	}
	page := RunPage{Items: make([]View, 0, min(c.Size, len(rows)))}
	for _, s := range rows[:min(c.Size, len(rows))] {
		page.Items = append(page.Items, View{ID: s.ID, WorkspaceID: s.WorkspaceID, State: s.State, Version: s.Version, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt})
	}
	if len(rows) > c.Size {
		last := rows[c.Size-1]
		c.BeforeCreated, c.BeforeID = last.CreatedAt, last.ID
		page.NextCursor, err = q.codec.Encode(c)
		if err != nil {
			return RunPage{}, ports.ErrUnavailable
		}
	}
	return page, nil
}

// ListEvents pins the upper Run revision on page one. Newer events require a
// fresh traversal. It is finite JSON pagination, not SSE or an Outbox consumer.
func (q *Queries) ListEvents(ctx context.Context, caller ports.Caller, id ports.RunID, request PageRequest) (EventPage, error) {
	if !id.IsValid() {
		return EventPage{}, ErrInvalidRequest
	}
	if err := q.authorize(ctx, caller, ports.ReadRunEvents, id); err != nil {
		return EventPage{}, err
	}
	c, err := q.continuation(caller, request, "events", "", id)
	if err != nil {
		return EventPage{}, err
	}
	batch, err := q.repository.FetchEvents(ctx, caller.WorkspaceID, id, ports.EventFilter{Size: c.Size, After: c.After, Through: c.Through})
	if err != nil {
		return EventPage{}, err
	}
	if err = ctx.Err(); err != nil {
		return EventPage{}, err
	}
	if batch.Through < 1 || batch.Through > math.MaxInt64 || (c.Through != 0 && batch.Through != c.Through) || len(batch.Items) > c.Size+1 {
		return EventPage{}, ports.ErrUnavailable
	}
	previous := c.After
	for _, event := range batch.Items {
		if event.WorkspaceID != caller.WorkspaceID || event.RunID != id || event.Version <= previous || event.Version < 2 || event.Version > batch.Through || !validState(string(event.State)) || event.State == domain.Queued || event.OccurredAt.IsZero() || event.OccurredAt.Year() < 1 || event.OccurredAt.Year() > 9999 || strings.TrimSpace(event.SubjectID) == "" || len(event.SubjectID) > 512 || !utf8.ValidString(event.Reason) || utf8.RuneCountInString(event.Reason) > 500 {
			return EventPage{}, ports.ErrUnavailable
		}
		previous = event.Version
	}
	page := EventPage{Items: append([]ports.Event{}, batch.Items[:min(c.Size, len(batch.Items))]...), ThroughVersion: batch.Through}
	if len(batch.Items) > c.Size {
		c.After, c.Through = batch.Items[c.Size-1].Version, batch.Through
		page.NextCursor, err = q.codec.Encode(c)
		if err != nil {
			return EventPage{}, ports.ErrUnavailable
		}
	}
	return page, nil
}
