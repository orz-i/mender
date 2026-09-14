package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

const ArtifactObjectThresholdBytes int64 = 256 << 10

var (
	ErrArtifactObjectUnavailable = errors.New("artifact object storage unavailable")
	ErrArtifactObjectConflict    = errors.New("artifact object conflicts with durable metadata")
	ErrNoArtifactObjectCandidate = errors.New("no artifact object candidate")
	ErrNoExpiredArtifactObject   = errors.New("no expired artifact object")
	ErrArtifactObjectNotFound    = errors.New("artifact object not found")
	ErrArtifactObjectExpired     = errors.New("artifact object expired")
	ErrArtifactCapabilityInvalid = errors.New("artifact object capability invalid")
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
	FindArtifactObject(context.Context, domain.WorkspaceID, domain.RunID, string) (domain.ArtifactObject, error)
	NextExpiredArtifactObject(context.Context, domain.WorkspaceID, time.Time) (domain.ArtifactObject, bool, error)
	ExpireArtifactObject(context.Context, domain.ArtifactObject, time.Time) (domain.ArtifactObject, error)
}

type ArtifactObjectStore interface {
	PutArtifactObject(context.Context, ArtifactObjectCandidate) (StoredArtifactObject, error)
	ReadArtifactObject(context.Context, string, string, int64) ([]byte, error)
	DeleteArtifactObject(context.Context, string) error
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

func (m *ArtifactObjectMaterializer) ExpireOne(ctx context.Context, workspace domain.WorkspaceID) (domain.ArtifactObject, error) {
	if err := ctx.Err(); err != nil {
		return domain.ArtifactObject{}, err
	}
	if !workspace.IsValid() {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	now := m.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	object, found, err := m.repository.NextExpiredArtifactObject(ctx, workspace, now)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	if !found {
		return domain.ArtifactObject{}, ErrNoExpiredArtifactObject
	}
	if object.Validate() != nil || object.WorkspaceID != workspace || object.State != domain.ArtifactObjectAvailable || now.Before(object.ExpiresAt) {
		return domain.ArtifactObject{}, ErrArtifactObjectUnavailable
	}
	if err = m.store.DeleteArtifactObject(ctx, object.ObjectKey); err != nil {
		return domain.ArtifactObject{}, err
	}
	expired, err := m.repository.ExpireArtifactObject(ctx, object, now)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	if expired.Validate() != nil || expired.State != domain.ArtifactObjectExpired || expired.WorkspaceID != object.WorkspaceID || expired.RunID != object.RunID || expired.ArtifactID != object.ArtifactID || expired.ObjectKey != object.ObjectKey || expired.ContentSHA256 != object.ContentSHA256 || expired.SizeBytes != object.SizeBytes || !expired.MaterializedAt.Equal(object.MaterializedAt) || !expired.ExpiresAt.Equal(object.ExpiresAt) || expired.DeletedAt.Before(now) {
		return domain.ArtifactObject{}, ErrArtifactObjectConflict
	}
	return expired, nil
}

type ArtifactObjectCapability struct {
	Version       int                `json:"v"`
	WorkspaceID   domain.WorkspaceID `json:"workspace_id"`
	RunID         domain.RunID       `json:"run_id"`
	ArtifactID    string             `json:"artifact_id"`
	ContentSHA256 string             `json:"content_sha256"`
	SizeBytes     int64              `json:"size_bytes"`
	ExpiresAt     time.Time          `json:"expires_at"`
}

type ArtifactObjectCapabilityCodec interface {
	EncodeArtifactObjectCapability(ArtifactObjectCapability) (string, error)
	DecodeArtifactObjectCapability(string) (ArtifactObjectCapability, error)
}

type ArtifactObjectGrant struct {
	Token     string
	ExpiresAt time.Time
}

type ArtifactObjectStatus struct {
	State     string
	ExpiresAt time.Time
}

type ArtifactObjectAccess struct {
	repository ArtifactObjectRepository
	store      ArtifactObjectStore
	authorizer ports.Authorizer
	codec      ArtifactObjectCapabilityCodec
	clock      ArtifactObjectClock
	ttl        time.Duration
}

func NewArtifactObjectAccess(repository ArtifactObjectRepository, store ArtifactObjectStore, authorizer ports.Authorizer, codec ArtifactObjectCapabilityCodec, clock ArtifactObjectClock, ttl time.Duration) (*ArtifactObjectAccess, error) {
	if repository == nil || store == nil || authorizer == nil || codec == nil || clock == nil || ttl < 30*time.Second || ttl > 5*time.Minute {
		return nil, ErrArtifactObjectUnavailable
	}
	return &ArtifactObjectAccess{repository: repository, store: store, authorizer: authorizer, codec: codec, clock: clock, ttl: ttl}, nil
}

func validArtifactObjectCaller(c ports.Caller) bool {
	return c.WorkspaceID.IsValid() && len(strings.TrimSpace(c.SubjectID)) >= 1 && len(c.SubjectID) <= 512 && len(strings.TrimSpace(c.CredentialID)) >= 1 && len(c.CredentialID) <= 128
}

func (a *ArtifactObjectAccess) Status(ctx context.Context, caller ports.Caller, runID domain.RunID, artifactID string) (ArtifactObjectStatus, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactObjectStatus{}, err
	}
	if a == nil || !validArtifactObjectCaller(caller) || !runID.IsValid() || !domain.ValidArtifactID(artifactID) {
		return ArtifactObjectStatus{}, ErrArtifactCapabilityInvalid
	}
	if err := a.authorizer.Authorize(ctx, caller, ports.ReadRun, runID); err != nil {
		return ArtifactObjectStatus{}, err
	}
	object, err := a.repository.FindArtifactObject(ctx, caller.WorkspaceID, runID, artifactID)
	if errors.Is(err, ErrArtifactObjectNotFound) {
		return ArtifactObjectStatus{State: "not_materialized"}, nil
	}
	if err != nil {
		return ArtifactObjectStatus{}, err
	}
	now := a.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() || object.Validate() != nil || object.WorkspaceID != caller.WorkspaceID || object.RunID != runID || object.ArtifactID != artifactID {
		return ArtifactObjectStatus{}, ErrArtifactObjectUnavailable
	}
	if object.State == domain.ArtifactObjectExpired || !now.Before(object.ExpiresAt) {
		return ArtifactObjectStatus{State: "expired", ExpiresAt: object.ExpiresAt}, nil
	}
	if object.State != domain.ArtifactObjectAvailable {
		return ArtifactObjectStatus{}, ErrArtifactObjectUnavailable
	}
	return ArtifactObjectStatus{State: "available", ExpiresAt: object.ExpiresAt}, nil
}

func validCapability(c ArtifactObjectCapability) bool {
	return c.Version == 1 && c.WorkspaceID.IsValid() && c.RunID.IsValid() && domain.ValidArtifactID(c.ArtifactID) && domain.ValidSHA256(c.ContentSHA256) && c.SizeBytes > ArtifactObjectThresholdBytes && c.SizeBytes <= 1<<20 && !c.ExpiresAt.IsZero() && c.ExpiresAt.Year() >= 1 && c.ExpiresAt.Year() <= 9999
}

func (a *ArtifactObjectAccess) Issue(ctx context.Context, caller ports.Caller, runID domain.RunID, artifactID string) (ArtifactObjectGrant, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactObjectGrant{}, err
	}
	if a == nil || !validArtifactObjectCaller(caller) || !runID.IsValid() || !domain.ValidArtifactID(artifactID) {
		return ArtifactObjectGrant{}, ErrArtifactCapabilityInvalid
	}
	if err := a.authorizer.Authorize(ctx, caller, ports.ReadRun, runID); err != nil {
		return ArtifactObjectGrant{}, err
	}
	object, err := a.repository.FindArtifactObject(ctx, caller.WorkspaceID, runID, artifactID)
	if err != nil {
		return ArtifactObjectGrant{}, err
	}
	now := a.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() || object.Validate() != nil || object.WorkspaceID != caller.WorkspaceID || object.RunID != runID || object.ArtifactID != artifactID {
		return ArtifactObjectGrant{}, ErrArtifactObjectUnavailable
	}
	if object.State != domain.ArtifactObjectAvailable || !now.Before(object.ExpiresAt) {
		return ArtifactObjectGrant{}, ErrArtifactObjectExpired
	}
	expiresAt := now.Add(a.ttl)
	if object.ExpiresAt.Before(expiresAt) {
		expiresAt = object.ExpiresAt
	}
	if !now.Before(expiresAt) {
		return ArtifactObjectGrant{}, ErrArtifactObjectExpired
	}
	capability := ArtifactObjectCapability{Version: 1, WorkspaceID: object.WorkspaceID, RunID: object.RunID, ArtifactID: object.ArtifactID, ContentSHA256: object.ContentSHA256, SizeBytes: object.SizeBytes, ExpiresAt: expiresAt}
	token, err := a.codec.EncodeArtifactObjectCapability(capability)
	if err != nil || len(token) < 1 || len(token) > 2048 {
		return ArtifactObjectGrant{}, ErrArtifactObjectUnavailable
	}
	return ArtifactObjectGrant{Token: token, ExpiresAt: expiresAt}, nil
}

func (a *ArtifactObjectAccess) Read(ctx context.Context, token string) ([]byte, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	if a == nil || len(token) < 1 || len(token) > 2048 {
		return nil, time.Time{}, ErrArtifactCapabilityInvalid
	}
	capability, err := a.codec.DecodeArtifactObjectCapability(token)
	if err != nil || !validCapability(capability) {
		return nil, time.Time{}, ErrArtifactCapabilityInvalid
	}
	now := a.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() || !now.Before(capability.ExpiresAt) || capability.ExpiresAt.After(now.Add(5*time.Minute)) {
		return nil, time.Time{}, ErrArtifactCapabilityInvalid
	}
	object, err := a.repository.FindArtifactObject(ctx, capability.WorkspaceID, capability.RunID, capability.ArtifactID)
	if err != nil {
		return nil, time.Time{}, err
	}
	if object.Validate() != nil || object.WorkspaceID != capability.WorkspaceID || object.RunID != capability.RunID || object.ArtifactID != capability.ArtifactID || object.ContentSHA256 != capability.ContentSHA256 || object.SizeBytes != capability.SizeBytes {
		return nil, time.Time{}, ErrArtifactCapabilityInvalid
	}
	if object.State != domain.ArtifactObjectAvailable || !now.Before(object.ExpiresAt) || capability.ExpiresAt.After(object.ExpiresAt) {
		return nil, time.Time{}, ErrArtifactObjectExpired
	}
	content, err := a.store.ReadArtifactObject(ctx, object.ObjectKey, object.ContentSHA256, object.SizeBytes)
	if err != nil {
		return nil, time.Time{}, err
	}
	if int64(len(content)) != object.SizeBytes || !utf8.Valid(content) || !json.Valid(content) {
		return nil, time.Time{}, ErrArtifactObjectUnavailable
	}
	return content, object.ExpiresAt, nil
}
