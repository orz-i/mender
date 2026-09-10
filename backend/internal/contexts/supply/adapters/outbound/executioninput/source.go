package executioninput

import (
	"context"
	"errors"

	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type Source struct{ inputs execution.RuntimeInputs }

func New(inputs execution.RuntimeInputs) *Source { return &Source{inputs: inputs} }

func (s *Source) ResolveExecutionInput(ctx context.Context, workspace, run string) (application.ExecutionInput, error) {
	if s == nil || s.inputs == nil {
		return application.ExecutionInput{}, application.ErrInvocationUnavailable
	}
	input, err := s.inputs.ResolveRuntimeInput(ctx, workspace, run)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return application.ExecutionInput{}, err
		default:
			return application.ExecutionInput{}, application.ErrInvocationUnavailable
		}
	}
	return application.ExecutionInput{WorkspaceID: input.WorkspaceID, RunID: input.RunID, SubjectID: input.SubjectID, ConnectionID: input.ConnectionID, ToolVersionID: input.ToolVersionID, DeploymentRevision: input.DeploymentRevision, CanonicalArguments: input.CanonicalArguments}, nil
}

var _ application.ExecutionInputSource = (*Source)(nil)
