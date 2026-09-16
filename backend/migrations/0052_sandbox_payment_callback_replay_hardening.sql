-- T27 replay hardening: qualify callback Inbox columns because the function's
-- TABLE output names include receipt_id/disposition/reason_code. Unqualified
-- receipt_id in the exact replay UPDATE is ambiguous in PL/pgSQL.

CREATE OR REPLACE FUNCTION commerce.ingest_sandbox_payment_callback(
    provider_value text,provider_account_value text,event_value text,body_digest text,key_value text,signed_time timestamptz,received_time timestamptz,
    requested_workspace text,intent_value text,event_type_value text,provider_transaction_value text,currency_value text,amount_value bigint,
    event_state_value text,occurred_time timestamptz)
RETURNS TABLE(receipt_id text,disposition text,reason_code text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt text; prior record; intent record; quarantine_reason text; expected_type text;
BEGIN
    IF provider_value !~ '^[A-Za-z0-9_-]{1,128}$' OR provider_account_value !~ '^[A-Za-z0-9_.:-]{1,128}$'
       OR event_value !~ '^[A-Za-z0-9_.:-]{1,200}$' OR body_digest !~ '^[a-f0-9]{64}$' OR key_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$' OR intent_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR event_type_value NOT IN ('payment.succeeded','payment.failed','refund.succeeded','refund.failed')
       OR currency_value !~ '^[A-Z]{3}$' OR amount_value<=0 OR event_state_value NOT IN ('succeeded','failed')
       OR signed_time IS NULL OR received_time<signed_time OR occurred_time IS NULL OR occurred_time>received_time+interval '1 minute'
       OR (event_state_value='succeeded')<>(provider_transaction_value IS NOT NULL)
       OR (provider_transaction_value IS NOT NULL AND provider_transaction_value !~ '^[A-Za-z0-9_.:-]{1,200}$') THEN
        RAISE EXCEPTION 'invalid verified payment callback' USING ERRCODE='22023';
    END IF;
    IF (event_type_value LIKE '%.succeeded' AND event_state_value<>'succeeded') OR (event_type_value LIKE '%.failed' AND event_state_value<>'failed') THEN
        RAISE EXCEPTION 'payment callback type/state mismatch' USING ERRCODE='22023';
    END IF;
    receipt:=encode(sha256(convert_to(provider_value||':'||provider_account_value||':'||event_value||':'||body_digest,'UTF8')),'hex');
    SELECT p.* INTO prior FROM commerce.payment_callback_inbox p
      WHERE p.provider_id=provider_value AND p.provider_account_id=provider_account_value AND p.event_id=event_value
      ORDER BY p.received_at,p.receipt_id LIMIT 1 FOR UPDATE;
    IF FOUND AND prior.body_sha256=body_digest AND prior.workspace_id=requested_workspace AND prior.intent_id=intent_value
       AND prior.event_type=event_type_value AND prior.provider_transaction_id IS NOT DISTINCT FROM provider_transaction_value
       AND prior.currency=currency_value AND prior.amount_micro=amount_value AND prior.event_state=event_state_value AND prior.occurred_at=occurred_time THEN
        UPDATE commerce.payment_callback_inbox AS p SET last_received_at=received_time,delivery_count=p.delivery_count+1 WHERE p.receipt_id=prior.receipt_id;
        RETURN QUERY SELECT prior.receipt_id,'duplicate'::text,NULL::text; RETURN;
    END IF;
    IF FOUND THEN quarantine_reason:='event_id_conflict'; END IF;
    SELECT p.* INTO intent FROM commerce.payment_intents p WHERE p.workspace_id=requested_workspace AND p.id=intent_value FOR UPDATE;
    IF quarantine_reason IS NULL AND NOT FOUND THEN quarantine_reason:='intent_not_found'; END IF;
    IF quarantine_reason IS NULL THEN
        expected_type:=CASE intent.purpose WHEN 'collect_charge' THEN CASE event_state_value WHEN 'succeeded' THEN 'payment.succeeded' ELSE 'payment.failed' END
                                                   ELSE CASE event_state_value WHEN 'succeeded' THEN 'refund.succeeded' ELSE 'refund.failed' END END;
        IF intent.mode<>'sandbox' THEN quarantine_reason:='live_mode_forbidden';
        ELSIF intent.provider_id<>provider_value OR intent.provider_account_id<>provider_account_value OR intent.currency<>currency_value OR intent.amount_micro<>amount_value OR expected_type<>event_type_value THEN quarantine_reason:='binding_mismatch';
        ELSIF intent.state<>'pending' THEN quarantine_reason:='intent_already_terminal';
        ELSIF occurred_time<intent.created_at THEN quarantine_reason:='event_before_intent';
        END IF;
    END IF;
    INSERT INTO commerce.payment_callback_inbox(receipt_id,provider_id,provider_account_id,event_id,body_sha256,key_id,signed_at,received_at,last_received_at,workspace_id,intent_id,event_type,provider_transaction_id,currency,amount_micro,event_state,occurred_at,disposition,reason_code,processed_at)
      VALUES(receipt,provider_value,provider_account_value,event_value,body_digest,key_value,signed_time,received_time,received_time,requested_workspace,intent_value,event_type_value,provider_transaction_value,currency_value,amount_value,event_state_value,occurred_time,
        CASE WHEN quarantine_reason IS NULL THEN 'accepted' ELSE 'quarantined' END,quarantine_reason,received_time);
    IF quarantine_reason IS NULL THEN
        UPDATE commerce.payment_intents AS p SET state=CASE event_state_value WHEN 'succeeded' THEN 'settled' ELSE 'failed' END,
          revision=2,provider_transaction_id=provider_transaction_value,last_provider_event_id=event_value,updated_at=occurred_time,
          settled_at=CASE WHEN event_state_value='succeeded' THEN occurred_time ELSE NULL END,
          failed_at=CASE WHEN event_state_value='failed' THEN occurred_time ELSE NULL END
        WHERE p.workspace_id=requested_workspace AND p.id=intent_value AND p.state='pending' AND p.revision=1;
        IF NOT FOUND THEN RAISE EXCEPTION 'payment intent CAS failed' USING ERRCODE='40001'; END IF;
        RETURN QUERY SELECT receipt,'accepted'::text,NULL::text; RETURN;
    END IF;
    RETURN QUERY SELECT receipt,'quarantined'::text,quarantine_reason;
END $$;

REVOKE ALL ON FUNCTION commerce.ingest_sandbox_payment_callback(text,text,text,text,text,timestamptz,timestamptz,text,text,text,text,text,bigint,text,timestamptz) FROM PUBLIC;
