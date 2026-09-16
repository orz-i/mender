-- T27 reconciliation hardening: qualify payment fact columns because TABLE
-- output parameters share workspace_id/provider_id/provider_account_id/currency.

CREATE OR REPLACE FUNCTION commerce.payment_reconciliation(
    requested_workspace text,actor_id text,provider_value text,provider_account_value text,currency_value text)
RETURNS TABLE(workspace_id text,provider_id text,provider_account_id text,currency text,
  expected_collection_micro bigint,settled_collection_micro bigint,expected_refund_micro bigint,settled_refund_micro bigint,
  pending_intent_count bigint,quarantined_event_count bigint,collection_difference_micro bigint,refund_difference_micro bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE ec bigint; sc bigint; er bigint; sr bigint; pending_count bigint; quarantined_count bigint;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$'
       OR provider_value !~ '^[A-Za-z0-9_-]{1,128}$' OR provider_account_value !~ '^[A-Za-z0-9_.:-]{1,128}$' OR currency_value !~ '^[A-Z]{3}$' THEN
        RAISE EXCEPTION 'payment reconciliation authority required' USING ERRCODE='42501';
    END IF;
    SELECT coalesce(sum(p.amount_micro) FILTER (WHERE p.purpose='collect_charge'),0),
           coalesce(sum(p.amount_micro) FILTER (WHERE p.purpose='collect_charge' AND p.state='settled'),0),
           coalesce(sum(p.amount_micro) FILTER (WHERE p.purpose='execute_refund'),0),
           coalesce(sum(p.amount_micro) FILTER (WHERE p.purpose='execute_refund' AND p.state='settled'),0),
           count(*) FILTER (WHERE p.state='pending')
      INTO ec,sc,er,sr,pending_count FROM commerce.payment_intents p
      WHERE p.workspace_id=requested_workspace AND p.provider_id=provider_value AND p.provider_account_id=provider_account_value AND p.currency=currency_value;
    SELECT count(*) INTO quarantined_count FROM commerce.payment_callback_inbox p
      WHERE p.workspace_id=requested_workspace AND p.provider_id=provider_value AND p.provider_account_id=provider_account_value AND p.currency=currency_value AND p.disposition='quarantined';
    RETURN QUERY SELECT requested_workspace,provider_value,provider_account_value,currency_value,ec,sc,er,sr,pending_count,quarantined_count,sc-ec,sr-er;
END $$;

REVOKE ALL ON FUNCTION commerce.payment_reconciliation(text,text,text,text,text) FROM PUBLIC;
