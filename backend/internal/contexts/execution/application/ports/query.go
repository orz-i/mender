package ports

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

// ReadRepository is a use-case-owned projection port, not an unscoped SQL gateway.
// Fetch methods return at most Size+1 rows to detect another page without COUNT(*).
type ReadRepository interface {
	FetchRuns(context.Context, WorkspaceID, RunFilter) ([]domain.Snapshot, error)
	FetchEvents(context.Context, WorkspaceID, RunID, EventFilter) (EventBatch, error)
}

type RunFilter struct {
	State         domain.State
	Size          int
	BeforeCreated time.Time
	BeforeID      RunID
}

type EventFilter struct {
	Size    int
	After   uint64
	Through uint64 // Zero establishes a watermark from the current Run revision.
}

type Event struct {
	WorkspaceID WorkspaceID
	RunID       RunID
	Version     uint64
	State       domain.State
	OccurredAt  time.Time
	SubjectID   string
	Reason      string
}

type EventBatch struct {
	Through uint64
	Items   []Event
}

// Cursor is an application continuation contract. It never grants access.
// The codec authenticates bytes; the use case enforces identity, filters and expiry.
type Cursor struct {
	Format        int         `json:"v"`
	Kind          string      `json:"kind"`
	Workspace     WorkspaceID `json:"workspace"`
	Subject       string      `json:"subject"`
	Credential    string      `json:"credential"`
	State         string      `json:"state"`
	Size          int         `json:"size"`
	Run           RunID       `json:"run"`
	BeforeCreated time.Time   `json:"before_created"`
	BeforeID      RunID       `json:"before_id"`
	After         uint64      `json:"after"`
	Through       uint64      `json:"through"`
	ExpiresAt     time.Time   `json:"expires_at"`
}

type CursorCodec interface {
	Encode(Cursor) (string, error)
	Decode(string) (Cursor, error)
}
