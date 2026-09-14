package domain

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidArtifactObject = errors.New("invalid artifact object")

type ArtifactObjectState string

const (
	ArtifactObjectAvailable ArtifactObjectState = "available"
	ArtifactObjectExpired   ArtifactObjectState = "expired"
)

type ArtifactObject struct {
	WorkspaceID    WorkspaceID
	RunID          RunID
	ArtifactID     string
	ObjectKey      string
	ContentSHA256  string
	SizeBytes      int64
	State          ArtifactObjectState
	MaterializedAt time.Time
	ExpiresAt      time.Time
	DeletedAt      time.Time
}

func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func ValidArtifactObjectKey(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 3 || parts[0] != "objects" || !validLowerHex(parts[1], 64) || !strings.HasSuffix(parts[2], ".json") {
		return false
	}
	return validLowerHex(strings.TrimSuffix(parts[2], ".json"), 64)
}

func ValidSHA256(value string) bool { return validLowerHex(value, 64) }

func (o ArtifactObject) Validate() error {
	if !o.WorkspaceID.IsValid() || !o.RunID.IsValid() || !ValidArtifactID(o.ArtifactID) || !ValidArtifactObjectKey(o.ObjectKey) || !ValidSHA256(o.ContentSHA256) || o.SizeBytes <= 256<<10 || o.SizeBytes > 1<<20 || !validJobTime(o.MaterializedAt) || !validJobTime(o.ExpiresAt) || !o.ExpiresAt.After(o.MaterializedAt) {
		return ErrInvalidArtifactObject
	}
	switch o.State {
	case ArtifactObjectAvailable:
		if !o.DeletedAt.IsZero() {
			return ErrInvalidArtifactObject
		}
	case ArtifactObjectExpired:
		if !validJobTime(o.DeletedAt) || o.DeletedAt.Before(o.ExpiresAt) {
			return ErrInvalidArtifactObject
		}
	default:
		return ErrInvalidArtifactObject
	}
	return nil
}
