package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
)

type RuntimeInputs struct {
	service *application.RuntimeInputService
}

func NewRuntimeInputs(service *application.RuntimeInputService) *RuntimeInputs {
	return &RuntimeInputs{service: service}
}

func (f *RuntimeInputs) ResolveRuntimeInput(ctx context.Context, workspace, run string) (execution.RuntimeInput, error) {
	input, err := f.service.Resolve(ctx, workspace, run)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrRuntimeInputNotFound):
			return execution.RuntimeInput{}, execution.ErrRuntimeInputNotFound
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return execution.RuntimeInput{}, err
		default:
			return execution.RuntimeInput{}, execution.ErrRuntimeInputUnavailable
		}
	}
	return execution.RuntimeInput{WorkspaceID: string(input.WorkspaceID), RunID: string(input.RunID), SubjectID: input.SubjectID, ConnectionID: input.ConnectionID, ToolVersionID: input.ToolVersionID, DeploymentRevision: input.DeploymentRevision, CanonicalArguments: input.CanonicalArguments}, nil
}

var _ execution.RuntimeInputs = (*RuntimeInputs)(nil)
