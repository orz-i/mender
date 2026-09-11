package domain

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

var ErrInvalidArtifact = errors.New("invalid artifact")

type ArtifactKind string

const ProviderResultArtifact ArtifactKind = "provider_result"

type Artifact struct {
	WorkspaceID         WorkspaceID
	RunID               RunID
	ID                  string
	Kind                ArtifactKind
	MediaType           string
	SourceObservationID string
	ContentJSON         string
	CreatedAt           time.Time
}

func validArtifactID(value string) bool {
	if len(value) < 1 || len(value) > 160 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func (a Artifact) Validate() error {
	if !a.WorkspaceID.IsValid() || !a.RunID.IsValid() || !validArtifactID(a.ID) || a.Kind != ProviderResultArtifact || a.MediaType != "application/json" || !validObservationID(a.SourceObservationID) || !validJobTime(a.CreatedAt) || len(a.ContentJSON) < 1 || len(a.ContentJSON) > 1<<20 || !utf8.ValidString(a.ContentJSON) || !json.Valid([]byte(a.ContentJSON)) {
		return ErrInvalidArtifact
	}
	return nil
}

func ArtifactForProviderResult(observation ProviderObservation) (Artifact, error) {
	if observation.Validate() != nil || observation.State != ProviderSucceeded {
		return Artifact{}, ErrInvalidArtifact
	}
	a := Artifact{
		WorkspaceID:         observation.WorkspaceID,
		RunID:               observation.RunID,
		ID:                  "art_" + string(observation.RunID),
		Kind:                ProviderResultArtifact,
		MediaType:           "application/json",
		SourceObservationID: observation.ObservationID,
		ContentJSON:         observation.ResultJSON,
		CreatedAt:           observation.ObservedAt,
	}
	if a.Validate() != nil {
		return Artifact{}, ErrInvalidArtifact
	}
	return a, nil
}
