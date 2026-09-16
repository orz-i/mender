package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func PaymentCallbackIngestorRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, ok bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("payment-callback-ingestor database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'commerce','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_function_privilege(current_user,'commerce.ingest_sandbox_payment_callback(text,text,text,text,text,timestamptz,timestamptz,text,text,text,text,text,bigint,text,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'commerce.create_sandbox_payment_intent(text,text,text,text,text,text,text,text,text,bigint,text,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'commerce.payment_reconciliation(text,text,text,text,text)','EXECUTE')`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("payment-callback-ingestor function grants are invalid")
	}
	for _, table := range []string{"commerce.payment_intents", "commerce.payment_callback_inbox", "commerce.billing_journals", "commerce.billing_entries", "identity.platform_staff", "execution.runs"} {
		if err = pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,n.oid,'USAGE') AND
		 (has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE'))
		 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname=$1`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("payment-callback-ingestor has direct business table authority: " + table)
		}
	}
	return nil
}
