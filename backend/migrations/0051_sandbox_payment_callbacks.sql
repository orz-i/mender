-- S4-06 / T27 sandbox payment boundary. Payment provider/region/merchant
-- eligibility is not approved, therefore only mode='sandbox' is representable.
-- Browser redirects are not persisted as payment truth. Verified callback
-- facts are stored without raw bodies, signatures or secret material.

CREATE TABLE commerce.payment_intents (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    business_key text NOT NULL CHECK (business_key ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_account_id text NOT NULL CHECK (provider_account_id ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    mode text NOT NULL CHECK (mode='sandbox'),
    purpose text NOT NULL CHECK (purpose IN ('collect_charge','execute_refund')),
    billing_journal_id text NOT NULL CHECK (billing_journal_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_micro bigint NOT NULL CHECK (amount_micro>0),
    state text NOT NULL CHECK (state IN ('pending','settled','failed')),
    revision bigint NOT NULL CHECK (revision>0),
    provider_transaction_id text CHECK (provider_transaction_id IS NULL OR provider_transaction_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    last_provider_event_id text CHECK (last_provider_event_id IS NULL OR last_provider_event_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at>=created_at),
    settled_at timestamptz,
    failed_at timestamptz,
    PRIMARY KEY(workspace_id,id),
    UNIQUE(workspace_id,business_key),
    FOREIGN KEY(workspace_id,billing_journal_id) REFERENCES commerce.billing_journals(workspace_id,id),
    CHECK (
      (state='pending' AND revision=1 AND provider_transaction_id IS NULL AND last_provider_event_id IS NULL AND settled_at IS NULL AND failed_at IS NULL)
      OR (state='settled' AND revision=2 AND provider_transaction_id IS NOT NULL AND last_provider_event_id IS NOT NULL AND settled_at IS NOT NULL AND failed_at IS NULL AND settled_at>=created_at)
      OR (state='failed' AND revision=2 AND provider_transaction_id IS NULL AND last_provider_event_id IS NOT NULL AND failed_at IS NOT NULL AND settled_at IS NULL AND failed_at>=created_at)
    )
);

CREATE TABLE commerce.payment_callback_inbox (
    receipt_id text PRIMARY KEY CHECK (receipt_id ~ '^[a-f0-9]{64}$'),
    provider_id text NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    provider_account_id text NOT NULL CHECK (provider_account_id ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    event_id text NOT NULL CHECK (event_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    body_sha256 text NOT NULL CHECK (body_sha256 ~ '^[a-f0-9]{64}$'),
    key_id text NOT NULL CHECK (key_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    signed_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL CHECK (received_at>=signed_at),
    last_received_at timestamptz NOT NULL CHECK (last_received_at>=received_at),
    delivery_count integer NOT NULL DEFAULT 1 CHECK (delivery_count BETWEEN 1 AND 1000000),
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    intent_id text NOT NULL CHECK (intent_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    event_type text NOT NULL CHECK (event_type IN ('payment.succeeded','payment.failed','refund.succeeded','refund.failed')),
    provider_transaction_id text CHECK (provider_transaction_id IS NULL OR provider_transaction_id ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_micro bigint NOT NULL CHECK (amount_micro>0),
    event_state text NOT NULL CHECK (event_state IN ('succeeded','failed')),
    occurred_at timestamptz NOT NULL,
    disposition text NOT NULL CHECK (disposition IN ('accepted','quarantined')),
    reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[A-Za-z0-9_.:-]{1,128}$'),
    processed_at timestamptz NOT NULL CHECK (processed_at>=received_at),
    UNIQUE(provider_id,provider_account_id,event_id,body_sha256),
    CHECK ((event_state='succeeded')=(provider_transaction_id IS NOT NULL)),
    CHECK ((disposition='accepted' AND reason_code IS NULL) OR (disposition='quarantined' AND reason_code IS NOT NULL))
);

CREATE INDEX payment_intent_reconciliation
  ON commerce.payment_intents(workspace_id,provider_id,provider_account_id,currency,state,updated_at,id);
CREATE INDEX payment_callback_event_lookup
  ON commerce.payment_callback_inbox(provider_id,provider_account_id,event_id,received_at,receipt_id);
CREATE INDEX payment_callback_admin_timeline
  ON commerce.payment_callback_inbox(workspace_id,received_at DESC,receipt_id DESC);

ALTER TABLE commerce.payment_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.payment_intents FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.payment_intents
  USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
  WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE commerce.payment_callback_inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.payment_callback_inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.payment_callback_inbox
  USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
  WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));

CREATE FUNCTION commerce.guard_payment_intent() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'payment intents are immutable records' USING ERRCODE='42501'; END IF;
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.business_key IS DISTINCT FROM OLD.business_key OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.provider_account_id IS DISTINCT FROM OLD.provider_account_id OR NEW.mode IS DISTINCT FROM OLD.mode
       OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.billing_journal_id IS DISTINCT FROM OLD.billing_journal_id
       OR NEW.currency IS DISTINCT FROM OLD.currency OR NEW.amount_micro IS DISTINCT FROM OLD.amount_micro
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'payment intent binding is immutable' USING ERRCODE='42501';
    END IF;
    IF OLD.state<>'pending' OR NEW.state NOT IN ('settled','failed') OR NEW.revision<>OLD.revision+1 OR NEW.updated_at<OLD.updated_at THEN
        RAISE EXCEPTION 'invalid payment intent transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_payment_intent BEFORE UPDATE OR DELETE ON commerce.payment_intents
 FOR EACH ROW EXECUTE FUNCTION commerce.guard_payment_intent();

CREATE FUNCTION commerce.guard_payment_callback() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'payment callback evidence is immutable' USING ERRCODE='42501'; END IF;
    IF NEW.receipt_id IS DISTINCT FROM OLD.receipt_id OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.provider_account_id IS DISTINCT FROM OLD.provider_account_id OR NEW.event_id IS DISTINCT FROM OLD.event_id
       OR NEW.body_sha256 IS DISTINCT FROM OLD.body_sha256 OR NEW.key_id IS DISTINCT FROM OLD.key_id
       OR NEW.signed_at IS DISTINCT FROM OLD.signed_at OR NEW.received_at IS DISTINCT FROM OLD.received_at
       OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.intent_id IS DISTINCT FROM OLD.intent_id
       OR NEW.event_type IS DISTINCT FROM OLD.event_type OR NEW.provider_transaction_id IS DISTINCT FROM OLD.provider_transaction_id
       OR NEW.currency IS DISTINCT FROM OLD.currency OR NEW.amount_micro IS DISTINCT FROM OLD.amount_micro
       OR NEW.event_state IS DISTINCT FROM OLD.event_state OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at
       OR NEW.disposition IS DISTINCT FROM OLD.disposition OR NEW.reason_code IS DISTINCT FROM OLD.reason_code
       OR NEW.processed_at IS DISTINCT FROM OLD.processed_at OR NEW.delivery_count<>OLD.delivery_count+1
       OR NEW.last_received_at<OLD.last_received_at THEN
        RAISE EXCEPTION 'invalid payment callback replay mutation' USING ERRCODE='42501';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER guard_payment_callback BEFORE UPDATE OR DELETE ON commerce.payment_callback_inbox
 FOR EACH ROW EXECUTE FUNCTION commerce.guard_payment_callback();

CREATE FUNCTION commerce.create_sandbox_payment_intent(
    requested_workspace text,intent_value text,business_key_value text,provider_value text,provider_account_value text,
    mode_value text,purpose_value text,billing_journal_value text,currency_value text,amount_value bigint,actor_id text,at_time timestamptz)
RETURNS TABLE(intent_id text,replay boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE existing record; journal record;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:operate')
       OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$' OR intent_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR business_key_value !~ '^[A-Za-z0-9_.:-]{1,200}$' OR provider_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR provider_account_value !~ '^[A-Za-z0-9_.:-]{1,128}$' OR mode_value<>'sandbox'
       OR purpose_value NOT IN ('collect_charge','execute_refund') OR billing_journal_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR currency_value !~ '^[A-Z]{3}$' OR amount_value<=0 OR at_time IS NULL THEN
        RAISE EXCEPTION 'sandbox payment intent forbidden' USING ERRCODE='42501';
    END IF;
    SELECT * INTO existing FROM commerce.payment_intents WHERE workspace_id=requested_workspace AND business_key=business_key_value;
    IF FOUND THEN
        IF existing.id=intent_value AND existing.provider_id=provider_value AND existing.provider_account_id=provider_account_value
           AND existing.mode=mode_value AND existing.purpose=purpose_value AND existing.billing_journal_id=billing_journal_value
           AND existing.currency=currency_value AND existing.amount_micro=amount_value THEN
            RETURN QUERY SELECT existing.id,true; RETURN;
        END IF;
        RAISE EXCEPTION 'payment business key conflict' USING ERRCODE='23505';
    END IF;
    SELECT * INTO journal FROM commerce.billing_journals WHERE workspace_id=requested_workspace AND id=billing_journal_value;
    IF NOT FOUND OR journal.currency<>currency_value OR journal.amount_micro<>amount_value
       OR (purpose_value='collect_charge' AND journal.journal_kind<>'charge')
       OR (purpose_value='execute_refund' AND journal.journal_kind<>'refund') THEN
        RAISE EXCEPTION 'payment intent billing binding mismatch' USING ERRCODE='23514';
    END IF;
    INSERT INTO commerce.payment_intents(workspace_id,id,business_key,provider_id,provider_account_id,mode,purpose,billing_journal_id,currency,amount_micro,state,revision,created_at,updated_at)
      VALUES(requested_workspace,intent_value,business_key_value,provider_value,provider_account_value,'sandbox',purpose_value,billing_journal_value,currency_value,amount_value,'pending',1,at_time,at_time);
    RETURN QUERY SELECT intent_value,false;
END $$;

CREATE FUNCTION commerce.ingest_sandbox_payment_callback(
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
    SELECT * INTO prior FROM commerce.payment_callback_inbox
      WHERE provider_id=provider_value AND provider_account_id=provider_account_value AND event_id=event_value
      ORDER BY received_at,receipt_id LIMIT 1 FOR UPDATE;
    IF FOUND AND prior.body_sha256=body_digest AND prior.workspace_id=requested_workspace AND prior.intent_id=intent_value
       AND prior.event_type=event_type_value AND prior.provider_transaction_id IS NOT DISTINCT FROM provider_transaction_value
       AND prior.currency=currency_value AND prior.amount_micro=amount_value AND prior.event_state=event_state_value AND prior.occurred_at=occurred_time THEN
        UPDATE commerce.payment_callback_inbox SET last_received_at=received_time,delivery_count=delivery_count+1 WHERE receipt_id=prior.receipt_id;
        RETURN QUERY SELECT prior.receipt_id,'duplicate'::text,NULL::text; RETURN;
    END IF;
    IF FOUND THEN quarantine_reason:='event_id_conflict'; END IF;
    SELECT * INTO intent FROM commerce.payment_intents WHERE workspace_id=requested_workspace AND id=intent_value FOR UPDATE;
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
        UPDATE commerce.payment_intents SET state=CASE event_state_value WHEN 'succeeded' THEN 'settled' ELSE 'failed' END,
          revision=2,provider_transaction_id=provider_transaction_value,last_provider_event_id=event_value,updated_at=occurred_time,
          settled_at=CASE WHEN event_state_value='succeeded' THEN occurred_time ELSE NULL END,
          failed_at=CASE WHEN event_state_value='failed' THEN occurred_time ELSE NULL END
        WHERE workspace_id=requested_workspace AND id=intent_value AND state='pending' AND revision=1;
        IF NOT FOUND THEN RAISE EXCEPTION 'payment intent CAS failed' USING ERRCODE='40001'; END IF;
        RETURN QUERY SELECT receipt,'accepted'::text,NULL::text; RETURN;
    END IF;
    RETURN QUERY SELECT receipt,'quarantined'::text,quarantine_reason;
END $$;

CREATE FUNCTION commerce.payment_reconciliation(
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
    SELECT coalesce(sum(amount_micro) FILTER (WHERE purpose='collect_charge'),0),
           coalesce(sum(amount_micro) FILTER (WHERE purpose='collect_charge' AND state='settled'),0),
           coalesce(sum(amount_micro) FILTER (WHERE purpose='execute_refund'),0),
           coalesce(sum(amount_micro) FILTER (WHERE purpose='execute_refund' AND state='settled'),0),
           count(*) FILTER (WHERE state='pending')
      INTO ec,sc,er,sr,pending_count FROM commerce.payment_intents
      WHERE workspace_id=requested_workspace AND provider_id=provider_value AND provider_account_id=provider_account_value AND currency=currency_value;
    SELECT count(*) INTO quarantined_count FROM commerce.payment_callback_inbox
      WHERE workspace_id=requested_workspace AND provider_id=provider_value AND provider_account_id=provider_account_value AND currency=currency_value AND disposition='quarantined';
    RETURN QUERY SELECT requested_workspace,provider_value,provider_account_value,currency_value,ec,sc,er,sr,pending_count,quarantined_count,sc-ec,sr-er;
END $$;

CREATE FUNCTION commerce.payment_intent_projection(requested_workspace text,actor_id text,provider_value text,provider_account_value text,limit_value integer)
RETURNS TABLE(id text,business_key text,purpose text,billing_journal_id text,currency text,amount_micro bigint,state text,revision bigint,created_at timestamptz,updated_at timestamptz,settled_at timestamptz,failed_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$'
       OR provider_value !~ '^[A-Za-z0-9_-]{1,128}$' OR provider_account_value !~ '^[A-Za-z0-9_.:-]{1,128}$' OR limit_value NOT BETWEEN 1 AND 200 THEN
        RAISE EXCEPTION 'payment intent audit authority required' USING ERRCODE='42501';
    END IF;
    RETURN QUERY SELECT p.id,p.business_key,p.purpose,p.billing_journal_id,p.currency,p.amount_micro,p.state,p.revision,p.created_at,p.updated_at,p.settled_at,p.failed_at
      FROM commerce.payment_intents p WHERE p.workspace_id=requested_workspace AND p.provider_id=provider_value AND p.provider_account_id=provider_account_value
      ORDER BY p.updated_at DESC,p.id DESC LIMIT limit_value;
END $$;

CREATE FUNCTION commerce.payment_callback_projection(requested_workspace text,actor_id text,provider_value text,provider_account_value text,limit_value integer)
RETURNS TABLE(receipt_id text,event_id text,intent_id text,event_type text,currency text,amount_micro bigint,event_state text,occurred_at timestamptz,disposition text,reason_code text,received_at timestamptz,delivery_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$'
       OR provider_value !~ '^[A-Za-z0-9_-]{1,128}$' OR provider_account_value !~ '^[A-Za-z0-9_.:-]{1,128}$' OR limit_value NOT BETWEEN 1 AND 200 THEN
        RAISE EXCEPTION 'payment callback audit authority required' USING ERRCODE='42501';
    END IF;
    RETURN QUERY SELECT p.receipt_id,p.event_id,p.intent_id,p.event_type,p.currency,p.amount_micro,p.event_state,p.occurred_at,p.disposition,p.reason_code,p.received_at,p.delivery_count
      FROM commerce.payment_callback_inbox p WHERE p.workspace_id=requested_workspace AND p.provider_id=provider_value AND p.provider_account_id=provider_account_value
      ORDER BY p.received_at DESC,p.receipt_id DESC LIMIT limit_value;
END $$;

REVOKE ALL ON commerce.payment_intents,commerce.payment_callback_inbox FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.guard_payment_intent() FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.guard_payment_callback() FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.create_sandbox_payment_intent(text,text,text,text,text,text,text,text,text,bigint,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.ingest_sandbox_payment_callback(text,text,text,text,text,timestamptz,timestamptz,text,text,text,text,text,bigint,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.payment_reconciliation(text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.payment_intent_projection(text,text,text,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.payment_callback_projection(text,text,text,text,integer) FROM PUBLIC;

COMMENT ON TABLE commerce.payment_callback_inbox IS
  'Verified sandbox payment callback facts only. Raw request bodies, signatures, secrets, authorization headers and credentials are intentionally excluded.';
