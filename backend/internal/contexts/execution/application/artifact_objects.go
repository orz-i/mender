package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

const ArtifactObjectThresholdBytes int64 = 256 << 10

var (
	ErrArtifactObjectUnavailable = errors.New("artifact object storage unavailable")
	ErrArtifactObjectConflict    = errors.New("artifact object conflicts with durable metadata")
	ErrNoArtifactObjectCandidate = errors.New("no artifact object candidate")
)

type ArtifactObjectCandidate struct {
	WorkspaceID domain.WorkspaceID
	RunID       domain.RunID
	ArtifactID  string
	MediaType   string
	ContentJSON string
	CreatedAt   time.Time
}

func (c ArtifactObjectCandidate) Valid() bool {
	size := int64(len(c.ContentJSON))
	return c.WorkspaceID.IsValid() && c.RunID.IsValid() && domain.ValidArtifactID(c.ArtifactID) && c.MediaType == "application/json" && size > ArtifactObjectThresholdBytes && size <= 1<<20 && utf8.ValidString(c.ContentJSON) && json.Valid([]byte(c.ContentJSON)) && !c.CreatedAt.IsZero()
}

type StoredArtifactObject struct {
	ObjectKey     string
	ContentSHA256 string
	SizeBytes     int64
}

type ArtifactObjectRepository interface {
	NextArtifactObjectCandidate(context.Context, domain.WorkspaceID) (ArtifactObjectCandidate, bool, error)
	RecordArtifactObject(context.Context, domain.ArtifactObject) (domain.ArtifactObject, error)
}

type ArtifactObjectStore interface {
	PutArtifactObject(context.Context, ArtifactObjectCandidate) (StoredArtifactObject, error)
}

type ArtifactObjectClock interface{ Now() time.Time }

type ArtifactObjectMaterializer struct {
	repository ArtifactObjectRepository
	store      ArtifactObjectStore
	clock      ArtifactObjectClock
	retention  time.Duration
}

func NewArtifactObjectMaterializer(repository ArtifactObjectRepository, store ArtifactObjectStore, clock ArtifactObjectClock, retention time.Duration) (*ArtifactObjectMaterializer, error) {
	if repository == nil || store == nil || clock == nil || retention < time.Hour || retention > 90*24*time.Hour {
		return nil, ErrArtifactObjectUnavailable
	}
	return &ArtifactObjectMaterializer{repository: repository, store: store, clock: clock, retention: retention}, nil
}

func sameArtifactObject(left, right domain.ArtifactObject) bool {
	return left.WorkspaceID == right.WorkspaceID && left.RunID == right.RunID && left.ArtifactID == right.ArtifactID && left.ObjectKey == right.ObjectKey && left.ContentSHA256 == right.ContentSHA256 && left.SizeBytes == right.SizeBytes && left.State == right.State && left.MaterializedAt.Equal(right.MaterializedAt) && left.ExpiresAt.Equal(right.ExpiresAt) && left.DeletedAt.Equal(right.DeletedAt)
}

func (m *ArtifactObjectMaterializer) MaterializeOne(ctx context.Context, workspace domain.WorkspaceID) (domain.ArtifactObject, error) {
	if err := ctx.Err(); err != nil {
		return domain.ArtifactObject{}, err
	}
	if !workspace.IsValid() {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	candidate, found, err := m.repository.NextArtifactObjectCandidate(ctx, workspace)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	if !found {
		return domain.ArtifactObject{}, ErrNoArtifactObjectCandidate
	}
	if !candidate.Valid() || candidate.WorkspaceID != workspace {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	stored, err := m.store.PutArtifactObject(ctx, candidate)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	now := m.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() || now.Before(candidate.CreatedAt) {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	object := domain.ArtifactObject{
		WorkspaceID: candidate.WorkspaceID, RunID: candidate.RunID, ArtifactID: candidate.ArtifactID,
		ObjectKey: stored.ObjectKey, ContentSHA256: stored.ContentSHA256, SizeBytes: stored.SizeBytes,
		State: domain.ArtifactObjectAvailable, MaterializedAt: now, ExpiresAt: now.Add(m.retention),
	}
	if object.Validate() != nil || object.SizeBytes != int64(len(candidate.ContentJSON)) {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	recorded, err := m.repository.RecordArtifactObject(ctx, object)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	if recorded.Validate() != nil || !sameArtifactObject(recorded, object) {
		return domain.ArtifactObject{}, ErrArtifactObjectConflict
	}
	return recorded, nil
}
