package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

type Pricing struct{ pool *pgxpool.Pool }

func NewPricing(pool *pgxpool.Pool) *Pricing { return &Pricing{pool: pool} }

func rollbackPricing(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (p *Pricing) FindPrice(ctx context.Context, id string) (domain.PriceVersion, error) {
	var price domain.PriceVersion
	err := p.pool.QueryRow(ctx, `SELECT id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active FROM commerce.price_versions WHERE id=$1`, id).Scan(&price.ID, &price.ToolVersionID, &price.Currency, &price.ReserveMicro, &price.ChargeMicro, &price.BillingPolicy, &price.StartsAt, &price.EndsAt, &price.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PriceVersion{}, application.ErrPriceUnavailable
	}
	if err != nil {
		return domain.PriceVersion{}, application.ErrPricingUnavailable
	}
	return price, nil
}

func (p *Pricing) FindBudgetWindow(ctx context.Context, workspace, budgetID, currency string, at time.Time) (domain.BudgetWindow, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.BudgetWindow{}, application.ErrPricingUnavailable
	}
	defer rollbackPricing(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return domain.BudgetWindow{}, application.ErrPricingUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT workspace_id,budget_id,period_id,currency,starts_at,ends_at,active FROM commerce.budget_periods WHERE workspace_id=$1 AND budget_id=$2 AND currency=$3 AND active AND starts_at<=$4 AND $4<ends_at ORDER BY starts_at DESC,period_id LIMIT 2`, workspace, budgetID, currency, at)
	if err != nil {
		return domain.BudgetWindow{}, application.ErrPricingUnavailable
	}
	defer rows.Close()
	windows := make([]domain.BudgetWindow, 0, 2)
	for rows.Next() {
		var b domain.BudgetWindow
		if err = rows.Scan(&b.WorkspaceID, &b.BudgetID, &b.PeriodID, &b.Currency, &b.StartsAt, &b.EndsAt, &b.Active); err != nil {
			return domain.BudgetWindow{}, application.ErrPricingUnavailable
		}
		windows = append(windows, b)
	}
	if err = rows.Err(); err != nil {
		return domain.BudgetWindow{}, application.ErrPricingUnavailable
	}
	if len(windows) != 1 {
		return domain.BudgetWindow{}, application.ErrPricingBudget
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.BudgetWindow{}, application.ErrPricingUnavailable
	}
	return windows[0], nil
}

var _ application.PricingRepository = (*Pricing)(nil)
