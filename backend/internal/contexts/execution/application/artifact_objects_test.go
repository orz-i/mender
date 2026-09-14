package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type objectTestClock struct{ at time.Time }

func (c objectTestClock) Now() time.Time { return c.at }

type objectTestRepo struct {
	candidate ArtifactObjectCandidate
	found     bool
	recorded  *domain.ArtifactObject
}

func (r *objectTestRepo) NextArtifactObjectCandidate(context.Context, domain.WorkspaceID) (ArtifactObjectCandidate, bool, error) {
	return r.candidate, r.found, nil
}
func (r *objectTestRepo) RecordArtifactObject(_ context.Context, object domain.ArtifactObject) (domain.ArtifactObject, error) {
	r.recorded = &object
	return object, nil
}

type objectTestStore struct {
	stored StoredArtifactObject
	calls  int
}

func (s *objectTestStore) PutArtifactObject(context.Context, ArtifactObjectCandidate) (StoredArtifactObject, error) {
	s.calls++
	return s.stored, nil
}

func largeArtifactCandidate() ArtifactObjectCandidate {
	content := `{"payload":"` + strings.Repeat("x", (256<<10)+64) + `"}`
	return ArtifactObjectCandidate{WorkspaceID: "ws_a", RunID: "run_a", ArtifactID: "art_run_a", MediaType: "application/json", ContentJSON: content, CreatedAt: time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)}
}

func TestArtifactObjectMaterializerRecordsOnlyValidatedDeterministicStoreFacts(t *testing.T) {
	candidate := largeArtifactCandidate()
	sha := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	store := &objectTestStore{stored: StoredArtifactObject{ObjectKey: "objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/" + sha + ".json", ContentSHA256: sha, SizeBytes: int64(len(candidate.ContentJSON))}}
	repo := &objectTestRepo{candidate: candidate, found: true}
	materializer, err := NewArtifactObjectMaterializer(repo, store, objectTestClock{at: candidate.CreatedAt.Add(time.Minute)}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	object, err := materializer.MaterializeOne(context.Background(), "ws_a")
	if err != nil || object.Validate() != nil || repo.recorded == nil || store.calls != 1 || !object.ExpiresAt.Equal(candidate.CreatedAt.Add(time.Minute+24*time.Hour)) {
		t.Fatal(object, err, store.calls)
	}

	repo.found = false
	if _, err = materializer.MaterializeOne(context.Background(), "ws_a"); !errors.Is(err, ErrNoArtifactObjectCandidate) {
		t.Fatal("missing candidate did not remain explicit", err)
	}
}

func TestArtifactObjectMaterializerFailsClosedOnStoreMetadataDrift(t *testing.T) {
	candidate := largeArtifactCandidate()
	repo := &objectTestRepo{candidate: candidate, found: true}
	store := &objectTestStore{stored: StoredArtifactObject{ObjectKey: "objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.json", ContentSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SizeBytes: 1}}
	materializer, err := NewArtifactObjectMaterializer(repo, store, objectTestClock{at: candidate.CreatedAt.Add(time.Minute)}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = materializer.MaterializeOne(context.Background(), "ws_a"); !errors.Is(err, ErrArtifactObjectUnavailable) || repo.recorded != nil {
		t.Fatal("store metadata drift reached durable metadata", err)
	}
}
