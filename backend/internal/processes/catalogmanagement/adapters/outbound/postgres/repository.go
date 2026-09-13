package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23514", "23503":
			return application.ErrConflict
		case "42501":
			return application.ErrForbidden
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrUnavailable
}

func (r *Repository) begin(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		rollback(tx)
		return nil, application.ErrUnavailable
	}
	return tx, nil
}

func scanTool(row pgx.Row) (application.ToolVersion, error) {
	var v application.ToolVersion
	var published, retired *time.Time
	err := row.Scan(&v.WorkspaceID, &v.ToolVersionID, &v.ToolID, &v.Version, &v.ProviderID, &v.PriceVersionID, &v.DeploymentRevision,
		&v.Title, &v.Description, &v.InputSchema, &v.OutputSchema, &v.SideEffect, &v.Idempotency, &v.MCPPublishable, &v.State, &v.CreatedAt, &v.UpdatedAt, &published, &retired)
	if published != nil {
		v.PublishedAt = *published
	}
	if retired != nil {
		v.RetiredAt = *retired
	}
	return v, mapErr(err)
}

const toolCols = `workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema::text,output_schema::text,side_effect,idempotency,mcp_publishable,state,created_at,updated_at,published_at,retired_at`

func (r *Repository) Snapshot(ctx context.Context, workspace string, at time.Time) (application.Snapshot, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.Snapshot{}, err
	}
	defer rollback(tx)
	out := application.Snapshot{ToolVersions: []application.ToolVersion{}, Toolsets: []application.Toolset{}, Connections: []application.ConnectionOption{}, Prices: []application.PriceOption{}, Budgets: []application.BudgetOption{}}
	rows, err := tx.Query(ctx, `SELECT `+toolCols+` FROM catalog.tool_version_management WHERE workspace_id=$1 ORDER BY tool_id,version`, workspace)
	if err != nil {
		return out, mapErr(err)
	}
	for rows.Next() {
		v, e := scanTool(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.ToolVersions = append(out.ToolVersions, v)
	}
	if rows.Err() != nil {
		rows.Close()
		return out, application.ErrUnavailable
	}
	rows.Close()
	toolsets := map[string]int{}
	rows, err = tx.Query(ctx, `SELECT workspace_id,id,state,created_at,updated_at,published_at,retired_at FROM distribution.toolsets WHERE workspace_id=$1 ORDER BY id`, workspace)
	if err != nil {
		return out, mapErr(err)
	}
	for rows.Next() {
		var s application.Toolset
		var p, ret *time.Time
		if rows.Scan(&s.WorkspaceID, &s.ID, &s.State, &s.CreatedAt, &s.UpdatedAt, &p, &ret) != nil {
			rows.Close()
			return out, application.ErrUnavailable
		}
		if p != nil {
			s.PublishedAt = *p
		}
		if ret != nil {
			s.RetiredAt = *ret
		}
		s.Bindings = []application.Binding{}
		toolsets[s.ID] = len(out.Toolsets)
		out.Toolsets = append(out.Toolsets, s)
	}
	if rows.Err() != nil {
		rows.Close()
		return out, application.ErrUnavailable
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,coalesce(connection_id,''),coalesce(mcp_name,''),mcp_exposed,state,published_at FROM distribution.toolset_bindings WHERE workspace_id=$1 ORDER BY toolset_version_id,tool_id,tool_version_label`, workspace)
	if err != nil {
		return out, mapErr(err)
	}
	for rows.Next() {
		var b application.Binding
		var p *time.Time
		if rows.Scan(&b.WorkspaceID, &b.ToolsetID, &b.ToolID, &b.ToolVersionLabel, &b.ToolVersionID, &b.BudgetID, &b.ConnectionID, &b.MCPName, &b.MCPExposed, &b.State, &p) != nil {
			rows.Close()
			return out, application.ErrUnavailable
		}
		if p != nil {
			b.PublishedAt = *p
		}
		if i, ok := toolsets[b.ToolsetID]; ok {
			out.Toolsets[i].Bindings = append(out.Toolsets[i].Bindings, b)
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return out, application.ErrUnavailable
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT id,provider_id,state,revision,created_at,expires_at FROM connections.connections WHERE workspace_id=$1 ORDER BY id`, workspace)
	if err != nil {
		return out, mapErr(err)
	}
	for rows.Next() {
		var v application.ConnectionOption
		if rows.Scan(&v.ConnectionID, &v.ProviderID, &v.State, &v.Revision, &v.CreatedAt, &v.ExpiresAt) != nil {
			rows.Close()
			return out, application.ErrUnavailable
		}
		out.Connections = append(out.Connections, v)
	}
	if rows.Err() != nil {
		rows.Close()
		return out, application.ErrUnavailable
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active FROM commerce.price_versions WHERE active AND starts_at<=$1 AND ends_at>$1 ORDER BY tool_version_id,id`, at)
	if err != nil {
		return out, mapErr(err)
	}
	for rows.Next() {
		var v application.PriceOption
		if rows.Scan(&v.ID, &v.ToolVersionID, &v.Currency, &v.ReserveMicro, &v.StartsAt, &v.EndsAt, &v.Active) != nil {
			rows.Close()
			return out, application.ErrUnavailable
		}
		out.Prices = append(out.Prices, v)
	}
	if rows.Err() != nil {
		rows.Close()
		return out, application.ErrUnavailable
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT budget_id,period_id,currency,starts_at,ends_at,active FROM commerce.budget_periods WHERE workspace_id=$1 AND active AND starts_at<=$2 AND ends_at>$2 ORDER BY budget_id,period_id`, workspace, at)
	if err != nil {
		return out, mapErr(err)
	}
	for rows.Next() {
		var v application.BudgetOption
		if rows.Scan(&v.BudgetID, &v.PeriodID, &v.Currency, &v.StartsAt, &v.EndsAt, &v.Active) != nil {
			rows.Close()
			return out, application.ErrUnavailable
		}
		out.Budgets = append(out.Budgets, v)
	}
	if rows.Err() != nil {
		rows.Close()
		return out, application.ErrUnavailable
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return out, application.ErrUnavailable
	}
	return out, nil
}

func (r *Repository) CreateToolVersion(ctx context.Context, workspace string, in application.ToolVersionInput) (application.ToolVersion, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.ToolVersion{}, err
	}
	defer rollback(tx)
	v, err := scanTool(tx.QueryRow(ctx, `INSERT INTO catalog.tool_version_management(workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12,$13,$14) RETURNING `+toolCols, workspace, in.ToolVersionID, in.ToolID, in.Version, in.ProviderID, in.PriceVersionID, in.DeploymentRevision, in.Title, in.Description, in.InputSchema, in.OutputSchema, in.SideEffect, in.Idempotency, in.MCPPublishable))
	if err != nil {
		return application.ToolVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ToolVersion{}, application.ErrUnavailable
	}
	return v, nil
}

func (r *Repository) UpdateToolVersion(ctx context.Context, workspace, id string, in application.ToolVersionInput) (application.ToolVersion, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.ToolVersion{}, err
	}
	defer rollback(tx)
	v, err := scanTool(tx.QueryRow(ctx, `UPDATE catalog.tool_version_management SET tool_id=$3,version=$4,provider_id=$5,price_version_id=$6,deployment_revision=$7,title=$8,description=$9,input_schema=$10::jsonb,output_schema=$11::jsonb,side_effect=$12,idempotency=$13,mcp_publishable=$14 WHERE workspace_id=$1 AND tool_version_id=$2 AND state='draft' RETURNING `+toolCols, workspace, id, in.ToolID, in.Version, in.ProviderID, in.PriceVersionID, in.DeploymentRevision, in.Title, in.Description, in.InputSchema, in.OutputSchema, in.SideEffect, in.Idempotency, in.MCPPublishable))
	if err != nil {
		return application.ToolVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ToolVersion{}, application.ErrUnavailable
	}
	return v, nil
}

func readIssues(ctx context.Context, tx pgx.Tx, query string, args ...any) (application.Preflight, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return application.Preflight{}, mapErr(err)
	}
	defer rows.Close()
	p := application.Preflight{Ready: true, Issues: []application.Issue{}}
	for rows.Next() {
		var i application.Issue
		if rows.Scan(&i.Code, &i.TargetID) != nil {
			return application.Preflight{}, application.ErrUnavailable
		}
		p.Issues = append(p.Issues, i)
	}
	if rows.Err() != nil {
		return application.Preflight{}, application.ErrUnavailable
	}
	p.Ready = len(p.Issues) == 0
	return p, nil
}

func (r *Repository) ToolVersionPreflight(ctx context.Context, workspace, id string, at time.Time) (application.Preflight, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.Preflight{}, err
	}
	defer rollback(tx)
	p, err := readIssues(ctx, tx, `SELECT code,target_id FROM catalog.tool_version_publish_issues($1,$2,$3)`, workspace, id, at)
	if err != nil {
		return p, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Preflight{}, application.ErrUnavailable
	}
	return p, nil
}
func (r *Repository) PublishToolVersion(ctx context.Context, workspace, id string, at time.Time) (application.ToolVersion, error) {
	return r.transitionTool(ctx, workspace, id, at, `SELECT catalog.publish_tool_version($1,$2,$3)`)
}
func (r *Repository) RetireToolVersion(ctx context.Context, workspace, id string, at time.Time) (application.ToolVersion, error) {
	return r.transitionTool(ctx, workspace, id, at, `SELECT catalog.retire_tool_version($1,$2,$3)`)
}
func (r *Repository) transitionTool(ctx context.Context, workspace, id string, at time.Time, statement string) (application.ToolVersion, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.ToolVersion{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, statement, workspace, id, at); err != nil {
		return application.ToolVersion{}, mapErr(err)
	}
	v, err := scanTool(tx.QueryRow(ctx, `SELECT `+toolCols+` FROM catalog.tool_version_management WHERE workspace_id=$1 AND tool_version_id=$2`, workspace, id))
	if err != nil {
		return application.ToolVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ToolVersion{}, application.ErrUnavailable
	}
	return v, nil
}

func scanToolset(row pgx.Row) (application.Toolset, error) {
	var s application.Toolset
	var p, r *time.Time
	err := row.Scan(&s.WorkspaceID, &s.ID, &s.State, &s.CreatedAt, &s.UpdatedAt, &p, &r)
	if p != nil {
		s.PublishedAt = *p
	}
	if r != nil {
		s.RetiredAt = *r
	}
	s.Bindings = []application.Binding{}
	return s, mapErr(err)
}

const toolsetCols = `workspace_id,id,state,created_at,updated_at,published_at,retired_at`

func (r *Repository) CreateToolset(ctx context.Context, workspace, id string) (application.Toolset, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.Toolset{}, err
	}
	defer rollback(tx)
	s, err := scanToolset(tx.QueryRow(ctx, `INSERT INTO distribution.toolsets(workspace_id,id) VALUES($1,$2) RETURNING `+toolsetCols, workspace, id))
	if err != nil {
		return application.Toolset{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Toolset{}, application.ErrUnavailable
	}
	return s, nil
}
func (r *Repository) UpsertBinding(ctx context.Context, workspace, toolset string, in application.BindingInput) (application.Binding, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.Binding{}, err
	}
	defer rollback(tx)
	var b application.Binding
	var p *time.Time
	err = tx.QueryRow(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed) VALUES($1,$2,$3,$4,$5,$6,$7,nullif($8,''),$9) ON CONFLICT(workspace_id,toolset_version_id,tool_version_id) DO UPDATE SET tool_id=excluded.tool_id,tool_version_label=excluded.tool_version_label,budget_id=excluded.budget_id,connection_id=excluded.connection_id,mcp_name=excluded.mcp_name,mcp_exposed=excluded.mcp_exposed WHERE distribution.toolset_bindings.state='draft' RETURNING workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,coalesce(connection_id,''),coalesce(mcp_name,''),mcp_exposed,state,published_at`, workspace, toolset, in.ToolID, in.ToolVersionLabel, in.ToolVersionID, in.BudgetID, in.ConnectionID, in.MCPName, in.MCPExposed).Scan(&b.WorkspaceID, &b.ToolsetID, &b.ToolID, &b.ToolVersionLabel, &b.ToolVersionID, &b.BudgetID, &b.ConnectionID, &b.MCPName, &b.MCPExposed, &b.State, &p)
	if err != nil {
		return application.Binding{}, mapErr(err)
	}
	if p != nil {
		b.PublishedAt = *p
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Binding{}, application.ErrUnavailable
	}
	return b, nil
}
func (r *Repository) DeleteBinding(ctx context.Context, workspace, toolset, toolVersion string) error {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return err
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, `DELETE FROM distribution.toolset_bindings WHERE workspace_id=$1 AND toolset_version_id=$2 AND tool_version_id=$3 AND state='draft'`, workspace, toolset, toolVersion)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrNotFound
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrUnavailable
	}
	return nil
}
func (r *Repository) ToolsetPreflight(ctx context.Context, workspace, id string, at time.Time) (application.Preflight, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.Preflight{}, err
	}
	defer rollback(tx)
	p, err := readIssues(ctx, tx, `SELECT code,target_id FROM distribution.toolset_publish_issues($1,$2,$3)`, workspace, id, at)
	if err != nil {
		return p, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Preflight{}, application.ErrUnavailable
	}
	return p, nil
}
func (r *Repository) PublishToolset(ctx context.Context, workspace, id string, at time.Time) (application.Toolset, error) {
	return r.transitionToolset(ctx, workspace, id, at, `SELECT distribution.publish_toolset($1,$2,$3)`)
}
func (r *Repository) RetireToolset(ctx context.Context, workspace, id string, at time.Time) (application.Toolset, error) {
	return r.transitionToolset(ctx, workspace, id, at, `SELECT distribution.retire_toolset($1,$2,$3)`)
}
func (r *Repository) transitionToolset(ctx context.Context, workspace, id string, at time.Time, statement string) (application.Toolset, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.Toolset{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, statement, workspace, id, at); err != nil {
		return application.Toolset{}, mapErr(err)
	}
	s, err := scanToolset(tx.QueryRow(ctx, `SELECT `+toolsetCols+` FROM distribution.toolsets WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return application.Toolset{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Toolset{}, application.ErrUnavailable
	}
	return s, nil
}

var _ application.Repository = (*Repository)(nil)
