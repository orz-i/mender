package domain

import (
	"testing"
	"time"
)

func TestArtifactObjectRequiresImmutableBoundedObjectMetadata(t *testing.T) {
	at := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	valid := ArtifactObject{
		WorkspaceID: "ws_a", RunID: "run_a", ArtifactID: "art_run_a",
		ObjectKey:     "objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.json",
		ContentSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SizeBytes: (256 << 10) + 1,
		State: ArtifactObjectAvailable, MaterializedAt: at, ExpiresAt: at.Add(24 * time.Hour),
	}
	if valid.Validate() != nil {
		t.Fatal("valid artifact object rejected")
	}
	invalid := []ArtifactObject{valid, valid, valid, valid, valid}
	invalid[0].ObjectKey = "../escape"
	invalid[1].ContentSHA256 = "ABC"
	invalid[2].SizeBytes = 256 << 10
	invalid[3].State, invalid[3].DeletedAt = ArtifactObjectExpired, at
	invalid[4].State, invalid[4].DeletedAt = ArtifactObjectAvailable, at.Add(25*time.Hour)
	for i, item := range invalid {
		if item.Validate() == nil {
			t.Fatalf("invalid artifact object accepted: %d", i)
		}
	}
	expired := valid
	expired.State, expired.DeletedAt = ArtifactObjectExpired, valid.ExpiresAt.Add(time.Minute)
	if expired.Validate() != nil {
		t.Fatal("valid expired object rejected")
	}
}
