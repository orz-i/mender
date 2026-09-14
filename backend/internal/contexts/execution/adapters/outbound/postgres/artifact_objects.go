package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

func (r *Repository) NextArtifactObjectCandidate(ctx context.Context, workspace domain.WorkspaceID) (application.ArtifactObjectCandidate, bool, error) {
	if err := ctx.Err(); err != nil {
		return application.ArtifactObjectCandidate{}, false, err
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.ArtifactObjectCandidate{}, false, err
	}
	defer rollback(tx)
	var candidate application.ArtifactObjectCandidate
	var workspaceID, runID string
	err = tx.QueryRow(ctx, `SELECT a.workspace_id,a.run_id,a.id,a.media_type,a.content_json::text,a.created_at
	 FROM execution.artifacts a
	 WHERE a.workspace_id=$1 AND a.kind='provider_result' AND a.media_type='application/json'
	   AND octet_length(a.content_json::text)>$2 AND octet_length(a.content_json::text)<=1048576
	   AND NOT EXISTS(SELECT 1 FROM execution.artifact_objects o WHERE o.workspace_id=a.workspace_id AND o.artifact_id=a.id)
	 ORDER BY a.created_at ASC,a.id COLLATE "C" ASC LIMIT 1`, string(workspace), application.ArtifactObjectThresholdBytes).Scan(&workspaceID, &runID, &candidate.ArtifactID, &candidate.MediaType, &candidate.ContentJSON, &candidate.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ArtifactObjectCandidate{}, false, nil
	}
	if err != nil {
		return application.ArtifactObjectCandidate{}, false, application.ErrArtifactObjectUnavailable
	}
	candidate.WorkspaceID, candidate.RunID = domain.WorkspaceID(workspaceID), domain.RunID(runID)
	if !candidate.Valid() || candidate.WorkspaceID != workspace {
		return application.ArtifactObjectCandidate{}, false, application.ErrArtifactObjectUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ArtifactObjectCandidate{}, false, application.ErrArtifactObjectUnavailable
	}
	return candidate, true, nil
}

func scanArtifactObject(row pgx.Row) (domain.ArtifactObject, error) {
	var object domain.ArtifactObject
	var workspaceID, runID, state string
	var deletedAt *time.Time
	err := row.Scan(&workspaceID, &runID, &object.ArtifactID, &object.ObjectKey, &object.ContentSHA256, &object.SizeBytes, &state, &object.MaterializedAt, &object.ExpiresAt, &deletedAt)
	object.WorkspaceID, object.RunID, object.State = domain.WorkspaceID(workspaceID), domain.RunID(runID), domain.ArtifactObjectState(state)
	if deletedAt != nil {
		object.DeletedAt = *deletedAt
	}
	return object, err
}

func (r *Repository) RecordArtifactObject(ctx context.Context, object domain.ArtifactObject) (domain.ArtifactObject, error) {
	if err := ctx.Err(); err != nil {
		return domain.ArtifactObject{}, err
	}
	if object.Validate() != nil || object.State != domain.ArtifactObjectAvailable {
		return domain.ArtifactObject{}, application.ErrArtifactObjectConflict
	}
	tx, err := r.scoped(ctx, object.WorkspaceID)
	if err != nil {
		return domain.ArtifactObject{}, err
	}
	defer rollback(tx)
	_, err = tx.Exec(ctx, `INSERT INTO execution.artifact_objects(workspace_id,run_id,artifact_id,object_key,content_sha256,size_bytes,state,materialized_at,expires_at)
	 VALUES($1,$2,$3,$4,$5,$6,'available',$7,$8) ON CONFLICT(workspace_id,artifact_id) DO NOTHING`, string(object.WorkspaceID), string(object.RunID), object.ArtifactID, object.ObjectKey, object.ContentSHA256, object.SizeBytes, object.MaterializedAt, object.ExpiresAt)
	if err != nil {
		return domain.ArtifactObject{}, application.ErrArtifactObjectUnavailable
	}
	stored, err := scanArtifactObject(tx.QueryRow(ctx, `SELECT workspace_id,run_id,artifact_id,object_key,content_sha256,size_bytes,state,materialized_at,expires_at,deleted_at
	 FROM execution.artifact_objects WHERE workspace_id=$1 AND artifact_id=$2`, string(object.WorkspaceID), object.ArtifactID))
	if err != nil || stored.Validate() != nil {
		return domain.ArtifactObject{}, application.ErrArtifactObjectUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ArtifactObject{}, application.ErrArtifactObjectUnavailable
	}
	return stored, nil
}
