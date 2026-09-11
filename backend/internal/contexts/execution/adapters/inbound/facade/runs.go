package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
)

type Runs struct {
	service *application.Service
	queries *application.Queries
}

func NewRuns(service *application.Service, queries *application.Queries) *Runs {
	return &Runs{service: service, queries: queries}
}

func executionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, application.ErrInvalidRequest):
		return execution.ErrInvalid
	case errors.Is(err, ports.ErrUnauthenticated):
		return execution.ErrUnauthenticated
	case errors.Is(err, ports.ErrForbidden):
		return execution.ErrForbidden
	case errors.Is(err, ports.ErrNotFound):
		return execution.ErrNotFound
	case errors.Is(err, ports.ErrConflict):
		return execution.ErrConflict
	case errors.Is(err, application.ErrOutcomeUnconfirmed):
		return execution.ErrOutcomeUnconfirmed
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return execution.ErrUnavailable
	}
}

func caller(c execution.Caller) ports.Caller {
	return ports.Caller{WorkspaceID: ports.WorkspaceID(c.WorkspaceID), SubjectID: c.SubjectID, CredentialID: c.CredentialID}
}

func runView(v application.View) execution.Run {
	return execution.Run{RunID: string(v.ID), WorkspaceID: string(v.WorkspaceID), State: string(v.State), Version: v.Version, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func (f *Runs) GetRun(ctx context.Context, c execution.Caller, runID string) (execution.Run, error) {
	if f == nil || f.service == nil {
		return execution.Run{}, execution.ErrUnavailable
	}
	v, err := f.service.GetRun(ctx, caller(c), ports.RunID(runID))
	if err != nil {
		return execution.Run{}, executionError(err)
	}
	return runView(v), nil
}

func (f *Runs) CancelRun(ctx context.Context, c execution.Caller, runID, reason string) (execution.Run, error) {
	if f == nil || f.service == nil {
		return execution.Run{}, execution.ErrUnavailable
	}
	v, err := f.service.CancelRunWithReason(ctx, caller(c), ports.RunID(runID), reason)
	if err != nil {
		return execution.Run{}, executionError(err)
	}
	return runView(v), nil
}

func (f *Runs) GetArtifact(ctx context.Context, c execution.Caller, runID, artifactID string) (execution.Artifact, error) {
	if f == nil || f.queries == nil {
		return execution.Artifact{}, execution.ErrUnavailable
	}
	v, err := f.queries.GetArtifact(ctx, caller(c), ports.RunID(runID), artifactID)
	if err != nil {
		return execution.Artifact{}, executionError(err)
	}
	return execution.Artifact{ArtifactID: v.ArtifactID, Kind: string(v.Kind), MediaType: v.MediaType, SizeBytes: v.SizeBytes, CreatedAt: v.CreatedAt, ContentJSON: v.ContentJSON}, nil
}

var _ execution.Runs = (*Runs)(nil)
