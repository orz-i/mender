package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func BillingManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, ok bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("billing-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_schema_privilege(current_user,'commerce','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_function_privilege(current_user,'commerce.billing_summary(text,text,text)','EXECUTE')
	 AND has_function_privilege(current_user,'commerce.billing_reconciliation(text,text,text)','EXECUTE')
	 AND has_function_privilege(current_user,'commerce.post_billing_refund(text,text,text,bigint,text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'commerce.post_billing_adjustment(text,text,text,text,text,bigint,text,text,text,text,timestamptz)','EXECUTE')`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("billing-manager function grants are invalid")
	}
	for _, table := range []string{
		"commerce.billing_journals", "commerce.billing_entries", "commerce.usage_settlements", "commerce.reservations", "commerce.budget_periods",
		"governance.dangerous_operation_approvals", "governance.dangerous_operation_audit_events", "identity.platform_staff", "execution.runs", "execution.run_admissions",
	} {
		if err = pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,n.oid,'USAGE') AND
		  (has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE'))
		 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname=$1`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("billing-manager has direct table authority: " + table)
		}
	}
	var governanceUsage, identityUsage, executionUsage, appendCharge bool
	err = pool.QueryRow(ctx, `SELECT
	 has_schema_privilege(current_user,'governance','USAGE'),
	 has_schema_privilege(current_user,'identity','USAGE'),
	 has_schema_privilege(current_user,'execution','USAGE'),
	 has_function_privilege(current_user,'commerce.append_usage_charge_journal(text,text,text,bigint,timestamptz)','EXECUTE')`).Scan(&governanceUsage, &identityUsage, &executionUsage, &appendCharge)
	if err != nil || governanceUsage || identityUsage || executionUsage || appendCharge {
		return errors.New("billing-manager isolation grants are invalid: governance_schema=" + boolString(governanceUsage) + " identity_schema=" + boolString(identityUsage) + " execution_schema=" + boolString(executionUsage) + " append_charge=" + boolString(appendCharge))
	}
	return nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
