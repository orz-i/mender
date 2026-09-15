package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

type PublicationRepository struct{ pool *pgxpool.Pool }

func NewPublicationRepository(pool *pgxpool.Pool) *PublicationRepository {
	return &PublicationRepository{pool: pool}
}

func rollbackPublication(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (r *PublicationRepository) scoped(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil || workspace == "" {
		return nil, application.ErrPublicationUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrPublicationUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		rollbackPublication(tx)
		return nil, application.ErrPublicationUnavailable
	}
	return tx, nil
}

func publicationWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23503", "23514":
			return application.ErrPublicationConflict
		case "42501":
			return application.ErrPublicationForbidden
		}
	}
	return application.ErrPublicationUnavailable
}

func scanPluginVersion(row pgx.Row) (application.PluginVersion, error) {
	var item application.PluginVersion
	var manifestJSON string
	var submittedAt, approvedAt, publishedAt, deprecatedAt, disabledAt *time.Time
	err := row.Scan(
		&item.WorkspaceID, &item.PluginID, &item.Version, &item.PublisherID, &item.Revision, &item.State,
		&manifestJSON, &item.ManifestSHA256, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
		&submittedAt, &approvedAt, &publishedAt, &deprecatedAt, &disabledAt,
	)
	if err != nil {
		return application.PluginVersion{}, err
	}
	if err = json.Unmarshal([]byte(manifestJSON), &item.Manifest); err != nil || item.Manifest.Domain().Validate() != nil || !domain.ValidPluginVersionState(domain.PluginVersionState(item.State)) {
		return application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	body, err := item.Manifest.Domain().CanonicalJSON()
	if err != nil {
		return application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if digest != item.ManifestSHA256 || item.Manifest.PluginID != item.PluginID || item.Manifest.Version != item.Version || item.Manifest.PublisherID != item.PublisherID || item.Revision < 1 {
		return application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	if submittedAt != nil {
		item.SubmittedAt = *submittedAt
	}
	if approvedAt != nil {
		item.ApprovedAt = *approvedAt
	}
	if publishedAt != nil {
		item.PublishedAt = *publishedAt
	}
	if deprecatedAt != nil {
		item.DeprecatedAt = *deprecatedAt
	}
	if disabledAt != nil {
		item.DisabledAt = *disabledAt
	}
	return item, nil
}

const pluginVersionProjection = `workspace_id,plugin_id,version,publisher_id,revision,state,manifest_json::text,manifest_sha256,created_by_user_id,created_at,updated_at,submitted_at,approved_at,published_at,deprecated_at,disabled_at`

func (r *PublicationRepository) Snapshot(ctx context.Context, workspace string) (application.PublisherSnapshot, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.PublisherSnapshot{}, err
	}
	defer rollbackPublication(tx)
	result := application.PublisherSnapshot{Publishers: []application.Publisher{}, Plugins: []application.Plugin{}, PluginVersions: []application.PluginVersion{}}

	rows, err := tx.Query(ctx, `SELECT workspace_id,id,owner_user_id,display_name,state,created_at,updated_at FROM supply.publishers WHERE workspace_id=$1 ORDER BY created_at,id`, workspace)
	if err != nil {
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	for rows.Next() {
		var item application.Publisher
		if err = rows.Scan(&item.WorkspaceID, &item.ID, &item.OwnerUserID, &item.DisplayName, &item.State, &item.CreatedAt, &item.UpdatedAt); err != nil || item.WorkspaceID != workspace {
			rows.Close()
			return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
		}
		result.Publishers = append(result.Publishers, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	rows.Close()

	rows, err = tx.Query(ctx, `SELECT workspace_id,id,publisher_id,created_by_user_id,created_at FROM supply.plugins WHERE workspace_id=$1 ORDER BY created_at,id`, workspace)
	if err != nil {
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	for rows.Next() {
		var item application.Plugin
		if err = rows.Scan(&item.WorkspaceID, &item.ID, &item.PublisherID, &item.CreatedByUserID, &item.CreatedAt); err != nil || item.WorkspaceID != workspace {
			rows.Close()
			return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
		}
		result.Plugins = append(result.Plugins, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	rows.Close()

	rows, err = tx.Query(ctx, `SELECT `+pluginVersionProjection+` FROM supply.plugin_versions WHERE workspace_id=$1 ORDER BY created_at,plugin_id,version`, workspace)
	if err != nil {
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	for rows.Next() {
		item, scanErr := scanPluginVersion(rows)
		if scanErr != nil || item.WorkspaceID != workspace {
			rows.Close()
			return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
		}
		result.PluginVersions = append(result.PluginVersions, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return application.PublisherSnapshot{}, application.ErrPublicationUnavailable
	}
	return result, nil
}

func (r *PublicationRepository) CreatePublisher(ctx context.Context, workspace, actor, publisherID, displayName string) (application.Publisher, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.Publisher{}, err
	}
	defer rollbackPublication(tx)
	var item application.Publisher
	err = tx.QueryRow(ctx, `INSERT INTO supply.publishers(workspace_id,id,owner_user_id,display_name) VALUES($1,$2,$3,$4) RETURNING workspace_id,id,owner_user_id,display_name,state,created_at,updated_at`, workspace, publisherID, actor, displayName).Scan(&item.WorkspaceID, &item.ID, &item.OwnerUserID, &item.DisplayName, &item.State, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return application.Publisher{}, publicationWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Publisher{}, application.ErrPublicationUnavailable
	}
	return item, nil
}

func (r *PublicationRepository) UpdatePublisher(ctx context.Context, workspace, actor, publisherID, displayName string) (application.Publisher, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.Publisher{}, err
	}
	defer rollbackPublication(tx)
	var item application.Publisher
	err = tx.QueryRow(ctx, `UPDATE supply.publishers SET display_name=$4,updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2 AND owner_user_id=$3 AND state='active' RETURNING workspace_id,id,owner_user_id,display_name,state,created_at,updated_at`, workspace, publisherID, actor, displayName).Scan(&item.WorkspaceID, &item.ID, &item.OwnerUserID, &item.DisplayName, &item.State, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.Publisher{}, application.ErrPublicationForbidden
	}
	if err != nil {
		return application.Publisher{}, publicationWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Publisher{}, application.ErrPublicationUnavailable
	}
	return item, nil
}

func (r *PublicationRepository) CreatePlugin(ctx context.Context, workspace, actor, publisherID, pluginID string) (application.Plugin, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.Plugin{}, err
	}
	defer rollbackPublication(tx)
	var item application.Plugin
	err = tx.QueryRow(ctx, `INSERT INTO supply.plugins(workspace_id,id,publisher_id,created_by_user_id)
 SELECT $1,$2,$3,$4 FROM supply.publishers p WHERE p.workspace_id=$1 AND p.id=$3 AND p.owner_user_id=$4 AND p.state='active'
 RETURNING workspace_id,id,publisher_id,created_by_user_id,created_at`, workspace, pluginID, publisherID, actor).Scan(&item.WorkspaceID, &item.ID, &item.PublisherID, &item.CreatedByUserID, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.Plugin{}, application.ErrPublicationForbidden
	}
	if err != nil {
		return application.Plugin{}, publicationWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.Plugin{}, application.ErrPublicationUnavailable
	}
	return item, nil
}

func manifestBody(manifest application.PluginManifest, expectedDigest string) (string, error) {
	body, err := manifest.Domain().CanonicalJSON()
	if err != nil {
		return "", application.ErrPublicationInvalid
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if digest != expectedDigest {
		return "", application.ErrPublicationInvalid
	}
	return string(body), nil
}

func (r *PublicationRepository) CreatePluginVersion(ctx context.Context, workspace, actor string, manifest application.PluginManifest, digest string) (application.PluginVersion, error) {
	body, err := manifestBody(manifest, digest)
	if err != nil {
		return application.PluginVersion{}, err
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.PluginVersion{}, err
	}
	defer rollbackPublication(tx)
	query := `INSERT INTO supply.plugin_versions(workspace_id,plugin_id,version,publisher_id,manifest_json,manifest_sha256,created_by_user_id)
 SELECT $1,$2,$3,$4,$5::jsonb,$6,$7
 FROM supply.plugins pl JOIN supply.publishers p ON (p.workspace_id,p.id)=(pl.workspace_id,pl.publisher_id)
 WHERE pl.workspace_id=$1 AND pl.id=$2 AND pl.publisher_id=$4 AND p.owner_user_id=$7 AND p.state='active'
 RETURNING ` + pluginVersionProjection
	item, scanErr := scanPluginVersion(tx.QueryRow(ctx, query, workspace, manifest.PluginID, manifest.Version, manifest.PublisherID, body, digest, actor))
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return application.PluginVersion{}, application.ErrPublicationForbidden
	}
	if scanErr != nil {
		return application.PluginVersion{}, publicationWriteError(scanErr)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	return item, nil
}

func (r *PublicationRepository) UpdatePluginVersion(ctx context.Context, workspace, actor, pluginID, version string, manifest application.PluginManifest, digest string) (application.PluginVersion, error) {
	body, err := manifestBody(manifest, digest)
	if err != nil {
		return application.PluginVersion{}, err
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.PluginVersion{}, err
	}
	defer rollbackPublication(tx)
	// Prefix each projected column because UPDATE ... FROM makes unqualified names ambiguous.
	query := `UPDATE supply.plugin_versions v SET manifest_json=$6::jsonb,manifest_sha256=$7
 FROM supply.publishers p
 WHERE v.workspace_id=$1 AND v.plugin_id=$2 AND v.version=$3 AND v.publisher_id=$4 AND v.state='draft'
   AND p.workspace_id=v.workspace_id AND p.id=v.publisher_id AND p.owner_user_id=$5 AND p.state='active'
 RETURNING v.workspace_id,v.plugin_id,v.version,v.publisher_id,v.revision,v.state,v.manifest_json::text,v.manifest_sha256,v.created_by_user_id,v.created_at,v.updated_at,v.submitted_at,v.approved_at,v.published_at,v.deprecated_at,v.disabled_at`
	item, scanErr := scanPluginVersion(tx.QueryRow(ctx, query, workspace, pluginID, version, manifest.PublisherID, actor, body, digest))
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return application.PluginVersion{}, application.ErrPublicationConflict
	}
	if scanErr != nil {
		return application.PluginVersion{}, publicationWriteError(scanErr)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	return item, nil
}

func (r *PublicationRepository) PluginVersionPreflight(ctx context.Context, workspace, actor, pluginID, version string) (application.PluginPreflight, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.PluginPreflight{}, err
	}
	defer rollbackPublication(tx)
	var owned bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM supply.plugin_versions v JOIN supply.publishers p ON (p.workspace_id,p.id)=(v.workspace_id,v.publisher_id)
 WHERE v.workspace_id=$1 AND v.plugin_id=$2 AND v.version=$3 AND p.owner_user_id=$4 AND p.state='active')`, workspace, pluginID, version, actor).Scan(&owned)
	if err != nil {
		return application.PluginPreflight{}, application.ErrPublicationUnavailable
	}
	if !owned {
		return application.PluginPreflight{}, application.ErrPublicationForbidden
	}
	rows, err := tx.Query(ctx, `SELECT code,target_id FROM supply.plugin_version_publish_issues($1,$2,$3)`, workspace, pluginID, version)
	if err != nil {
		return application.PluginPreflight{}, publicationWriteError(err)
	}
	result := application.PluginPreflight{Ready: true, Issues: []application.PublicationIssue{}}
	for rows.Next() {
		var issue application.PublicationIssue
		if err = rows.Scan(&issue.Code, &issue.TargetID); err != nil {
			rows.Close()
			return application.PluginPreflight{}, application.ErrPublicationUnavailable
		}
		result.Ready = false
		result.Issues = append(result.Issues, issue)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return application.PluginPreflight{}, application.ErrPublicationUnavailable
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return application.PluginPreflight{}, application.ErrPublicationUnavailable
	}
	return result, nil
}

func fetchPluginVersion(ctx context.Context, tx pgx.Tx, workspace, pluginID, version string) (application.PluginVersion, error) {
	item, err := scanPluginVersion(tx.QueryRow(ctx, `SELECT `+pluginVersionProjection+` FROM supply.plugin_versions WHERE workspace_id=$1 AND plugin_id=$2 AND version=$3`, workspace, pluginID, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.PluginVersion{}, application.ErrPublicationNotFound
	}
	if err != nil {
		return application.PluginVersion{}, publicationWriteError(err)
	}
	return item, nil
}

func (r *PublicationRepository) SubmitPluginPublication(ctx context.Context, workspace, actor, pluginID, version, approvalID string, at, expires time.Time) (application.PluginVersion, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.PluginVersion{}, err
	}
	defer rollbackPublication(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.submit_plugin_publication($1,$2,$3,$4,$5,$6,$7)`, workspace, approvalID, pluginID, version, actor, at, expires); err != nil {
		return application.PluginVersion{}, publicationWriteError(err)
	}
	item, err := fetchPluginVersion(ctx, tx, workspace, pluginID, version)
	if err != nil {
		return application.PluginVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	return item, nil
}

func (r *PublicationRepository) PublishPluginPublication(ctx context.Context, workspace, actor, pluginID, version string, at time.Time) (string, application.PluginVersion, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return "", application.PluginVersion{}, err
	}
	defer rollbackPublication(tx)
	var approvalID *string
	if err = tx.QueryRow(ctx, `SELECT governance.publish_approved_plugin($1,$2,$3,$4,$5)`, workspace, pluginID, version, actor, at).Scan(&approvalID); err != nil {
		return "", application.PluginVersion{}, publicationWriteError(err)
	}
	if approvalID == nil || *approvalID == "" {
		return "", application.PluginVersion{}, application.ErrPublicationConflict
	}
	item, err := fetchPluginVersion(ctx, tx, workspace, pluginID, version)
	if err != nil {
		return "", application.PluginVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", application.PluginVersion{}, application.ErrPublicationUnavailable
	}
	return *approvalID, item, nil
}

var _ application.PublisherRepository = (*PublicationRepository)(nil)
var _ application.PluginPublicationWorkflowRepository = (*PublicationRepository)(nil)
