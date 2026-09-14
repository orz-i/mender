package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type ProviderCallbackInbox struct{ pool *pgxpool.Pool }

func NewProviderCallbackInbox(pool *pgxpool.Pool) *ProviderCallbackInbox {
	return &ProviderCallbackInbox{pool: pool}
}

func (r *ProviderCallbackInbox) ListProviderCallbackInbox(ctx context.Context, workspace string, filter application.ProviderCallbackInboxFilter) (application.ProviderCallbackInboxPage, error) {
	if r == nil || r.pool == nil {
		return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
	}
	var before any
	if !filter.BeforeReceivedAt.IsZero() {
		before = filter.BeforeReceivedAt
	}
	rows, err := tx.Query(ctx, `SELECT receipt_id,workspace_id,provider_id,event_id,run_id,observation_id,observation_state,disposition,COALESCE(reason_code,''),delivery_count,delivery_count-1,received_at,last_received_at,observed_at,processed_at
	 FROM execution.provider_callback_inbox
	 WHERE workspace_id=$1 AND ($2='' OR provider_id=$2) AND ($3='' OR disposition=$3) AND ($4='' OR reason_code=$4)
	 AND ($5::timestamptz IS NULL OR received_at<$5 OR (received_at=$5 AND receipt_id<$6))
	 ORDER BY received_at DESC,receipt_id DESC LIMIT $7`, workspace, filter.ProviderID, filter.Disposition, filter.ReasonCode, before, filter.BeforeReceiptID, filter.Limit+1)
	if err != nil {
		return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
	}
	defer rows.Close()
	items := make([]application.ProviderCallbackInboxItem, 0, filter.Limit+1)
	for rows.Next() {
		var item application.ProviderCallbackInboxItem
		var processed *time.Time
		if err = rows.Scan(&item.ReceiptID, &item.WorkspaceID, &item.ProviderID, &item.EventID, &item.RunID, &item.ObservationID, &item.ObservationState, &item.Disposition, &item.ReasonCode,
			&item.DeliveryCount, &item.DuplicateDeliveryCount, &item.ReceivedAt, &item.LastReceivedAt, &item.ObservedAt, &processed); err != nil {
			return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
		}
		if processed != nil {
			item.ProcessedAt = processed.UTC()
		}
		item.ReceivedAt, item.LastReceivedAt, item.ObservedAt = item.ReceivedAt.UTC(), item.LastReceivedAt.UTC(), item.ObservedAt.UTC()
		items = append(items, item)
	}
	if rows.Err() != nil {
		return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
	}
	page := application.ProviderCallbackInboxPage{}
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		last := items[len(items)-1]
		page.NextBeforeReceivedAt, page.NextBeforeReceiptID = last.ReceivedAt, last.ReceiptID
	}
	page.Items = items
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderCallbackInboxPage{}, application.ErrUnavailable
	}
	return page, nil
}

var _ application.ProviderCallbackInboxRepository = (*ProviderCallbackInbox)(nil)
