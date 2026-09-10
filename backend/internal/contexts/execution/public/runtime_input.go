package public

import (
	"context"
	"errors"
)

var (
	ErrRuntimeInputNotFound    = errors.New("execution runtime input not found")
	ErrRuntimeInputUnavailable = errors.New("execution runtime input unavailable")
)

type RuntimeInput struct {
	WorkspaceID, RunID, SubjectID, ConnectionID, ToolVersionID, DeploymentRevision string
	CanonicalArguments                                                             string
}

type RuntimeInputs interface {
	ResolveRuntimeInput(context.Context, string, string) (RuntimeInput, error)
}
