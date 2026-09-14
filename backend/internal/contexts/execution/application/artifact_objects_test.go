package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type objectTestClock struct{ at time.Time }

func (c objectTestClock) Now() time.Time { return c.at }

type objectTestRepo struct {
	candidate    ArtifactObjectCandidate
	found        bool
	recorded     *domain.ArtifactObject
	object       domain.ArtifactObject
	expiredFound bool
}

func (r *objectTestRepo) NextArtifactObjectCandidate(context.Context, domain.WorkspaceID) (ArtifactObjectCandidate, bool, error) {
	return r.candidate, r.found, nil
}
func (r *objectTestRepo) RecordArtifactObject(_ context.Context, object domain.ArtifactObject) (domain.ArtifactObject, error) {
	r.recorded = &object
	r.object = object
	return object, nil
}
func (r *objectTestRepo) FindArtifactObject(context.Context, domain.WorkspaceID, domain.RunID, string) (domain.ArtifactObject, error) {
	if r.object.Validate() != nil {
		return domain.ArtifactObject{}, ErrArtifactObjectNotFound
	}
	return r.object, nil
}
func (r *objectTestRepo) NextExpiredArtifactObject(context.Context, domain.WorkspaceID, time.Time) (domain.ArtifactObject, bool, error) {
	return r.object, r.expiredFound, nil
}
func (r *objectTestRepo) ExpireArtifactObject(_ context.Context, object domain.ArtifactObject, at time.Time) (domain.ArtifactObject, error) {
	object.State = domain.ArtifactObjectExpired
	object.DeletedAt = at
	r.object = object
	return object, nil
}

type objectTestStore struct {
	stored  StoredArtifactObject
	content []byte
	calls   int
	deletes int
}

func (s *objectTestStore) PutArtifactObject(context.Context, ArtifactObjectCandidate) (StoredArtifactObject, error) {
	s.calls++
	return s.stored, nil
}
func (s *objectTestStore) ReadArtifactObject(context.Context, string, string, int64) ([]byte, error) {
	return append([]byte(nil), s.content...), nil
}
func (s *objectTestStore) DeleteArtifactObject(context.Context, string) error {
	s.deletes++
	return nil
}

type objectTestAuth struct {
	err   error
	calls int
}

func (a *objectTestAuth) Authorize(context.Context, ports.Caller, ports.Action, domain.RunID) error {
	a.calls++
	return a.err
}

type objectTestCodec struct {
	value                ArtifactObjectCapability
	token                string
	encodeErr, decodeErr error
}

func (c *objectTestCodec) EncodeArtifactObjectCapability(value ArtifactObjectCapability) (string, error) {
	c.value = value
	return c.token, c.encodeErr
}
func (c *objectTestCodec) DecodeArtifactObjectCapability(string) (ArtifactObjectCapability, error) {
	return c.value, c.decodeErr
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

func accessObject(at time.Time, content string) domain.ArtifactObject {
	return domain.ArtifactObject{
		WorkspaceID: "ws_a", RunID: "run_a", ArtifactID: "art_run_a",
		ObjectKey:     "objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.json",
		ContentSHA256: strings.Repeat("b", 64), SizeBytes: int64(len(content)), State: domain.ArtifactObjectAvailable,
		MaterializedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour),
	}
}

func TestArtifactObjectAccessAuthorizesIssueAndRevalidatesSignedRead(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	content := `{"payload":"` + strings.Repeat("x", (256<<10)+32) + `"}`
	object := accessObject(now, content)
	repo := &objectTestRepo{object: object}
	store := &objectTestStore{content: []byte(content)}
	auth := &objectTestAuth{}
	codec := &objectTestCodec{token: "signed-capability"}
	access, err := NewArtifactObjectAccess(repo, store, auth, codec, objectTestClock{at: now}, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	caller := ports.Caller{WorkspaceID: "ws_a", SubjectID: "subject_a", CredentialID: "cred_a"}
	grant, err := access.Issue(context.Background(), caller, "run_a", "art_run_a")
	if err != nil || grant.Token != "signed-capability" || !grant.ExpiresAt.Equal(now.Add(2*time.Minute)) || auth.calls != 1 || codec.value.ContentSHA256 != object.ContentSHA256 || codec.value.SizeBytes != object.SizeBytes {
		t.Fatal(grant, err, auth.calls, codec.value)
	}
	body, expiresAt, err := access.Read(context.Background(), grant.Token)
	if err != nil || string(body) != content || !expiresAt.Equal(object.ExpiresAt) {
		t.Fatal(len(body), expiresAt, err)
	}
	status, err := access.Status(context.Background(), caller, "run_a", "art_run_a")
	if err != nil || status.State != "available" || !status.ExpiresAt.Equal(object.ExpiresAt) {
		t.Fatal("available object status drift", status, err)
	}
}

func TestArtifactObjectAccessFailsClosedAfterObjectExpiry(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	content := `{"payload":"` + strings.Repeat("x", (256<<10)+32) + `"}`
	object := accessObject(now, content)
	object.ExpiresAt = now
	repo := &objectTestRepo{object: object}
	store := &objectTestStore{content: []byte(content)}
	auth := &objectTestAuth{}
	codec := &objectTestCodec{token: "signed-capability", value: ArtifactObjectCapability{Version: 1, WorkspaceID: "ws_a", RunID: "run_a", ArtifactID: "art_run_a", ContentSHA256: object.ContentSHA256, SizeBytes: object.SizeBytes, ExpiresAt: now.Add(time.Minute)}}
	access, err := NewArtifactObjectAccess(repo, store, auth, codec, objectTestClock{at: now}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	caller := ports.Caller{WorkspaceID: "ws_a", SubjectID: "subject_a", CredentialID: "cred_a"}
	if _, err = access.Issue(context.Background(), caller, "run_a", "art_run_a"); !errors.Is(err, ErrArtifactObjectExpired) {
		t.Fatal("expired object issued a capability", err)
	}
	if _, _, err = access.Read(context.Background(), "signed-capability"); !errors.Is(err, ErrArtifactObjectExpired) && !errors.Is(err, ErrArtifactCapabilityInvalid) {
		t.Fatal("expired object content remained readable", err)
	}
	status, err := access.Status(context.Background(), caller, "run_a", "art_run_a")
	if err != nil || status.State != "expired" || !status.ExpiresAt.Equal(object.ExpiresAt) {
		t.Fatal("expired object status drift", status, err)
	}
}

func TestArtifactObjectMaterializerExpiresOnlyAfterPhysicalDelete(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	content := `{"payload":"` + strings.Repeat("x", (256<<10)+32) + `"}`
	object := accessObject(now, content)
	object.ExpiresAt = now.Add(-time.Minute)
	repo := &objectTestRepo{object: object, expiredFound: true}
	store := &objectTestStore{}
	materializer, err := NewArtifactObjectMaterializer(repo, store, objectTestClock{at: now}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := materializer.ExpireOne(context.Background(), "ws_a")
	if err != nil || store.deletes != 1 || expired.State != domain.ArtifactObjectExpired || expired.DeletedAt.Before(now) {
		t.Fatal(expired, err, store.deletes)
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
