package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

func (r *Repository) FetchRuns(ctx context.Context, workspace ports.WorkspaceID, f ports.RunFilter) ([]domain.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Size < 1 || f.Size > 100 || (!f.BeforeCreated.IsZero() && !f.BeforeID.IsValid()) || f.BeforeCreated.IsZero() && f.BeforeID != "" {
		return nil, ports.ErrUnavailable
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	query := `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1`
	args := []any{string(workspace)}
	if f.State != "" {
		args = append(args, string(f.State))
		query += fmt.Sprintf(" AND state=$%d", len(args))
	}
	if !f.BeforeCreated.IsZero() {
		args = append(args, f.BeforeCreated.UTC().Truncate(time.Microsecond), string(f.BeforeID))
		query += fmt.Sprintf(` AND (created_at,id COLLATE "C") < ($%d::timestamptz,$%d::text COLLATE "C")`, len(args)-1, len(args))
	}
	args = append(args, f.Size+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC,id COLLATE "C" DESC LIMIT $%d`, len(args))
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, ports.ErrUnavailable
	}
	defer rows.Close()
	items := make([]domain.Snapshot, 0, f.Size+1)
	for rows.Next() {
		var s domain.Snapshot
		var wid, id, state string
		var version int64
		if err = rows.Scan(&wid, &id, &state, &version, &s.CreatedAt, &s.UpdatedAt); err != nil || version < 1 {
			return nil, ports.ErrUnavailable
		}
		s.WorkspaceID = domain.WorkspaceID(wid)
		s.ID = domain.RunID(id)
		s.State = domain.State(state)
		s.Version = uint64(version)
		if _, err = domain.Restore(s); err != nil {
			return nil, ports.ErrUnavailable
		}
		items = append(items, s)
	}
	if err = rows.Err(); err != nil {
		return nil, ports.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, ports.ErrUnavailable
	}
	return items, nil
}

func (r *Repository) FetchEvents(ctx context.Context, workspace ports.WorkspaceID, id ports.RunID, f ports.EventFilter) (ports.EventBatch, error) {
	if err := ctx.Err(); err != nil {
		return ports.EventBatch{}, err
	}
	if !id.IsValid() {
		return ports.EventBatch{}, ports.ErrNotFound
	}
	if f.Size < 1 || f.Size > 100 || f.After > math.MaxInt64 || f.Through > math.MaxInt64 || f.Through == 0 && f.After != 0 {
		return ports.EventBatch{}, ports.ErrUnavailable
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return ports.EventBatch{}, err
	}
	defer rollback(tx)
	var current int64
	err = tx.QueryRow(ctx, `SELECT version FROM execution.runs WHERE workspace_id=$1 AND id=$2`, string(workspace), string(id)).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.EventBatch{}, ports.ErrNotFound
	}
	if err != nil || current < 1 {
		return ports.EventBatch{}, ports.ErrUnavailable
	}
	through := f.Through
	if through == 0 {
		through = uint64(current)
	}
	if through > uint64(current) || f.After >= through {
		return ports.EventBatch{}, ports.ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT workspace_id,run_id,version,state,occurred_at,subject_id,reason FROM execution.run_events
	 WHERE workspace_id=$1 AND run_id=$2 AND version>$3 AND version<=$4 ORDER BY version ASC LIMIT $5`, string(workspace), string(id), int64(f.After), int64(through), f.Size+1)
	if err != nil {
		return ports.EventBatch{}, ports.ErrUnavailable
	}
	defer rows.Close()
	batch := ports.EventBatch{Through: through, Items: make([]ports.Event, 0, f.Size+1)}
	for rows.Next() {
		var e ports.Event
		var wid, rid, state string
		var version int64
		if err = rows.Scan(&wid, &rid, &version, &state, &e.OccurredAt, &e.SubjectID, &e.Reason); err != nil || version < 2 {
			return ports.EventBatch{}, ports.ErrUnavailable
		}
		e.WorkspaceID = ports.WorkspaceID(wid)
		e.RunID = ports.RunID(rid)
		e.State = domain.State(state)
		e.Version = uint64(version)
		batch.Items = append(batch.Items, e)
	}
	if err = rows.Err(); err != nil {
		return ports.EventBatch{}, ports.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return ports.EventBatch{}, ports.ErrUnavailable
	}
	return batch, nil
}

func (r *Repository) FetchArtifacts(ctx context.Context, workspace ports.WorkspaceID, id ports.RunID) ([]ports.ArtifactMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !id.IsValid() {
		return nil, ports.ErrNotFound
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution.runs WHERE workspace_id=$1 AND id=$2)`, string(workspace), string(id)).Scan(&exists); err != nil {
		return nil, ports.ErrUnavailable
	}
	if !exists {
		return nil, ports.ErrNotFound
	}
	rows, err := tx.Query(ctx, `SELECT workspace_id,run_id,id,kind,media_type,octet_length(content_json::text),created_at
	 FROM execution.artifacts WHERE workspace_id=$1 AND run_id=$2 ORDER BY created_at ASC,id COLLATE "C" ASC LIMIT 101`, string(workspace), string(id))
	if err != nil {
		return nil, ports.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ports.ArtifactMetadata, 0, 2)
	for rows.Next() {
		var item ports.ArtifactMetadata
		var workspaceID, runID, kind string
		if err = rows.Scan(&workspaceID, &runID, &item.ArtifactID, &kind, &item.MediaType, &item.SizeBytes, &item.CreatedAt); err != nil {
			return nil, ports.ErrUnavailable
		}
		item.WorkspaceID, item.RunID, item.Kind = ports.WorkspaceID(workspaceID), ports.RunID(runID), domain.ArtifactKind(kind)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, ports.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, ports.ErrUnavailable
	}
	return items, nil
}

func (r *Repository) FetchArtifact(ctx context.Context, workspace ports.WorkspaceID, id ports.RunID, artifactID string) (ports.ArtifactRecord, error) {
	if err := ctx.Err(); err != nil {
		return ports.ArtifactRecord{}, err
	}
	if !id.IsValid() || !domain.ValidArtifactID(artifactID) {
		return ports.ArtifactRecord{}, ports.ErrNotFound
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return ports.ArtifactRecord{}, err
	}
	defer rollback(tx)
	var record ports.ArtifactRecord
	var workspaceID, runID, kind string
	err = tx.QueryRow(ctx, `SELECT workspace_id,run_id,id,kind,media_type,octet_length(content_json::text),created_at,content_json::text
	 FROM execution.artifacts WHERE workspace_id=$1 AND run_id=$2 AND id=$3`, string(workspace), string(id), artifactID).Scan(&workspaceID, &runID, &record.ArtifactID, &kind, &record.MediaType, &record.SizeBytes, &record.CreatedAt, &record.ContentJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ArtifactRecord{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.ArtifactRecord{}, ports.ErrUnavailable
	}
	record.WorkspaceID, record.RunID, record.Kind = ports.WorkspaceID(workspaceID), ports.RunID(runID), domain.ArtifactKind(kind)
	if err = tx.Commit(ctx); err != nil {
		return ports.ArtifactRecord{}, ports.ErrUnavailable
	}
	return record, nil
}

var _ ports.ReadRepository = (*Repository)(nil)
