package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func CommerceObserverRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("commerce-observer database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'commerce.budget_periods','limit_micro','SELECT')
	 AND has_column_privilege(current_user,'commerce.budget_periods','consumed_micro','SELECT')
	 AND has_column_privilege(current_user,'commerce.budget_periods','reserved_micro','SELECT')
	 AND has_column_privilege(current_user,'commerce.reservations','amount_micro','SELECT')
	 AND has_column_privilege(current_user,'commerce.reservations','charged_micro','SELECT')
	 AND has_column_privilege(current_user,'commerce.reservations','released_at','SELECT')
	 AND has_column_privilege(current_user,'commerce.reservations','settled_at','SELECT')
	 AND has_column_privilege(current_user,'commerce.usage_settlements','outcome','SELECT')
	 AND NOT has_column_privilege(current_user,'commerce.usage_settlements','reservation_id','SELECT')
	 AND NOT has_column_privilege(current_user,'commerce.usage_settlements','price_version_id','SELECT')
	 AND NOT has_table_privilege(current_user,'commerce.budget_periods','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'commerce.reservations','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'commerce.usage_settlements','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("commerce-observer database grants do not match the restricted contract")
	}
	return nil
}
