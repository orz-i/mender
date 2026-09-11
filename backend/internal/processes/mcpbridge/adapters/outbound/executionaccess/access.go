package executionaccess

import (
	"context"
	"errors"

	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type Access struct{ runs execution.Runs }

func New(runs execution.Runs) *Access { return &Access{runs: runs} }

func executionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, execution.ErrInvalid):
		return application.ErrInvalid
	case errors.Is(err, execution.ErrUnauthenticated):
		return application.ErrUnauthenticated
	case errors.Is(err, execution.ErrForbidden):
		return application.ErrForbidden
	case errors.Is(err, execution.ErrNotFound):
		return application.ErrNotFound
	case errors.Is(err, execution.ErrConflict):
		return application.ErrConflict
	case errors.Is(err, execution.ErrOutcomeUnconfirmed):
		return application.ErrOutcomeUnconfirmed
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrUnavailable
	}
}

func caller(c application.Caller) execution.Caller {
	return execution.Caller{WorkspaceID: c.WorkspaceID, SubjectID: c.SubjectID, CredentialID: c.CredentialID}
}

func run(r execution.Run) application.Run {
	return application.Run{RunID: r.RunID, WorkspaceID: r.WorkspaceID, State: r.State, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (a *Access) GetRun(ctx context.Context, c application.Caller, runID string) (application.Run, error) {
	if a == nil || a.runs == nil {
		return application.Run{}, application.ErrUnavailable
	}
	r, err := a.runs.GetRun(ctx, caller(c), runID)
	if err != nil {
		return application.Run{}, executionError(err)
	}
	return run(r), nil
}

func (a *Access) CancelRun(ctx context.Context, c application.Caller, runID, reason string) (application.Run, error) {
	if a == nil || a.runs == nil {
		return application.Run{}, application.ErrUnavailable
	}
	r, err := a.runs.CancelRun(ctx, caller(c), runID, reason)
	if err != nil {
		return application.Run{}, executionError(err)
	}
	return run(r), nil
}

func (a *Access) GetArtifact(ctx context.Context, c application.Caller, runID, artifactID string) (application.Artifact, error) {
	if a == nil || a.runs == nil {
		return application.Artifact{}, application.ErrUnavailable
	}
	r, err := a.runs.GetArtifact(ctx, caller(c), runID, artifactID)
	if err != nil {
		return application.Artifact{}, executionError(err)
	}
	return application.Artifact{ArtifactID: r.ArtifactID, Kind: r.Kind, MediaType: r.MediaType, SizeBytes: r.SizeBytes, CreatedAt: r.CreatedAt, ContentJSON: r.ContentJSON}, nil
}

var _ application.Runs = (*Access)(nil)
