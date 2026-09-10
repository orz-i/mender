package domain

import (
	"errors"
	"unicode/utf8"
)

var ErrInvalidRuntimeInput = errors.New("invalid execution runtime input")

type RuntimeInput struct {
	WorkspaceID        WorkspaceID
	RunID              RunID
	SubjectID          string
	ConnectionID       string
	ToolVersionID      string
	DeploymentRevision string
	CanonicalArguments string
}

func (r RuntimeInput) Validate() error {
	if !r.WorkspaceID.IsValid() || !r.RunID.IsValid() || !validID(r.SubjectID) || !validID(r.ConnectionID) || !validID(r.ToolVersionID) || !validID(r.DeploymentRevision) || len(r.CanonicalArguments) < 2 || len(r.CanonicalArguments) > 65536 || !utf8.ValidString(r.CanonicalArguments) {
		return ErrInvalidRuntimeInput
	}
	return nil
}
