-- S4-03 operational billing ledger. Existing reservations/usage settlements
-- remain quota/usage facts; this ledger records immutable balanced billing
-- transfers and supplemental corrections. It is not a payment-provider or
-- statutory accounting general ledger.

ALTER TABLE governance.dangerous_operation_approvals
  DROP CONSTRAINT dangerous_operation_approvals_action_check;
ALTER TABLE governance.dangerous_operation_approvals
  ADD CONSTRAINT dangerous_operation_approvals_action_check CHECK (
    action IN ('release.emergency_disable','support.workspace_read','commerce.refund','commerce.adjustment'));
ALTER TABLE governance.dangerous_operation_approvals
  DROP CONSTRAINT dangerous_operation_approvals_target_kind_check;
ALTER TABLE governance.dangerous_operation_approvals
  ADD CONSTRAINT dangerous_operation_approvals_target_kind_check CHECK (
    target_kind IN ('release_plan','workspace','billing_refund','billing_adjustment'));

CREATE OR REPLACE FUNCTION governance.dangerous_requester_authorized(
    requested_workspace text,action_name text,user_id_value text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM identity.users u WHERE u.id=user_id_value AND NOT u.disabled) THEN RETURN false; END IF;
    IF action_name='release.emergency_disable' THEN
        RETURN EXISTS(SELECT 1 FROM identity.workspace_memberships m
          WHERE m.workspace_id=requested_workspace AND m.user_id=user_id_value AND NOT m.disabled AND m.role IN ('owner','admin'));
    ELSIF action_name='support.workspace_read' THEN
        RETURN EXISTS(SELECT 1 FROM identity.platform_staff s
          WHERE s.user_id=user_id_value AND NOT s.disabled AND s.role IN ('support','operator'));
    ELSIF action_name IN ('commerce.refund','commerce.adjustment') THEN
        RETURN EXISTS(SELECT 1 FROM identity.platform_staff s
          WHERE s.user_id=user_id_value AND NOT s.disabled AND s.role='operator');
    END IF;
    RETURN false;
END $$;

CREATE OR REPLACE FUNCTION governance.submit_dangerous_operation(
    requested_workspace text,approval text,requester_id text,subject_kind_value text,subject_id_value text,
    action_name text,target_kind_value text,target_id_value text,target_version_value text,
    parameters jsonb,parameters_digest text,amount_value bigint,currency_value text,reason_text text,
    at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE release_rev bigint;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    IF approval !~ '^[A-Za-z0-9_-]{1,128}$' OR requester_id !~ '^[A-Za-z0-9_-]{1,128}$'
       OR subject_id_value<>requester_id OR parameters_digest !~ '^[a-f0-9]{64}$'
       OR jsonb_typeof(parameters)<>'object' OR octet_length(parameters::text)>8192
       OR char_length(reason_text) NOT BETWEEN 1 AND 1000
       OR expiry_time<at_time+interval '1 minute' OR expiry_time>at_time+interval '30 minutes'
       OR (amount_value IS NULL)<>(currency_value IS NULL)
       OR (amount_value IS NOT NULL AND (amount_value<0 OR currency_value !~ '^[A-Z]{3}$'))
       OR NOT governance.dangerous_requester_authorized(requested_workspace,action_name,requester_id) THEN
        RAISE EXCEPTION 'invalid dangerous operation request' USING ERRCODE='42501';
    END IF;
    IF action_name='release.emergency_disable' THEN
        IF subject_kind_value<>'workspace_member' OR target_kind_value<>'release_plan'
           OR parameters<>'{"mode":"emergency_disable"}'::jsonb THEN
            RAISE EXCEPTION 'invalid release emergency binding' USING ERRCODE='22023';
        END IF;
        SELECT revision INTO release_rev FROM supply.release_plans
         WHERE workspace_id=requested_workspace AND id=target_id_value;
        IF release_rev IS NULL OR target_version_value<>release_rev::text THEN
            RAISE EXCEPTION 'release target version changed' USING ERRCODE='23514';
        END IF;
    ELSIF action_name='support.workspace_read' THEN
        IF subject_kind_value<>'platform_staff' OR target_kind_value<>'workspace' OR target_id_value<>requested_workspace
           OR target_version_value<>'' OR NOT governance.valid_support_parameters(parameters) THEN
            RAISE EXCEPTION 'invalid support JIT binding' USING ERRCODE='22023';
        END IF;
    ELSIF action_name IN ('commerce.refund','commerce.adjustment') THEN
        IF subject_kind_value<>'platform_staff' OR amount_value IS NULL OR amount_value<=0 OR currency_value IS NULL
           OR target_id_value !~ '^[A-Za-z0-9_-]{1,128}$' OR target_version_value !~ '^[A-Za-z0-9_.:-]{1,200}$'
           OR (action_name='commerce.refund' AND target_kind_value<>'billing_refund')
           OR (action_name='commerce.adjustment' AND target_kind_value<>'billing_adjustment') THEN
            RAISE EXCEPTION 'invalid commerce dangerous binding' USING ERRCODE='22023';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported dangerous operation' USING ERRCODE='22023';
    END IF;
    INSERT INTO governance.dangerous_operation_approvals(
      workspace_id,id,requester_user_id,subject_kind,subject_id,action,target_kind,target_id,target_version,
      parameters_json,parameters_sha256,amount_micro,currency,reason,state,requested_at,expires_at)
    VALUES(requested_workspace,approval,requester_id,subject_kind_value,subject_id_value,action_name,target_kind_value,target_id_value,target_version_value,
      parameters,parameters_digest,amount_value,currency_value,reason_text,'pending',at_time,expiry_time);
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'requested',requester_id,at_time,reason_text);
END $$;

CREATE OR REPLACE FUNCTION governance.dangerous_reviewer_authorized(
    requested_workspace text,action_name text,user_id_value text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM identity.users u WHERE u.id=user_id_value AND NOT u.disabled) THEN RETURN false; END IF;
    IF action_name='release.emergency_disable' THEN
        RETURN EXISTS(SELECT 1 FROM identity.workspace_memberships m
          WHERE m.workspace_id=requested_workspace AND m.user_id=user_id_value AND NOT m.disabled AND m.role IN ('owner','admin'));
    ELSIF action_name IN ('support.workspace_read','commerce.refund','commerce.adjustment') THEN
        RETURN EXISTS(SELECT 1 FROM identity.platform_staff s
          WHERE s.user_id=user_id_value AND NOT s.disabled AND s.role IN ('reviewer','operator'));
    END IF;
    RETURN false;
END $$;

CREATE TABLE commerce.billing_journals (
    workspace_id text NOT NULL CHECK (workspace_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    id text NOT NULL CHECK (id ~ '^[A-Za-z0-9_-]{1,128}$'),
    business_key text NOT NULL CHECK (business_key ~ '^[A-Za-z0-9_.:-]{1,200}$'),
    journal_kind text NOT NULL CHECK (journal_kind IN ('charge','refund','adjustment')),
    direction text NOT NULL CHECK (direction IN ('','debit','credit')),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_micro bigint NOT NULL CHECK (amount_micro>=0),
    basis_kind text NOT NULL CHECK (basis_kind IN ('usage_settlement','run','incident','reconciliation')),
    basis_id text NOT NULL CHECK (basis_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    approval_id text CHECK (approval_id IS NULL OR approval_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    actor_user_id text CHECK (actor_user_id IS NULL OR actor_user_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    occurred_at timestamptz NOT NULL CHECK (occurred_at>='0001-01-01 UTC' AND occurred_at<'10000-01-01 UTC'),
    PRIMARY KEY(workspace_id,id),
    UNIQUE(workspace_id,business_key),
    FOREIGN KEY(workspace_id,approval_id) REFERENCES governance.dangerous_operation_approvals(workspace_id,id),
    CHECK (
      (journal_kind='charge' AND direction='' AND basis_kind='usage_settlement' AND approval_id IS NULL AND actor_user_id IS NULL AND reason='usage settlement')
      OR (journal_kind='refund' AND direction='credit' AND basis_kind='usage_settlement' AND approval_id IS NOT NULL AND actor_user_id IS NOT NULL)
      OR (journal_kind='adjustment' AND direction IN ('debit','credit') AND basis_kind IN ('run','incident','reconciliation') AND approval_id IS NOT NULL AND actor_user_id IS NOT NULL)
    )
);

CREATE TABLE commerce.billing_entries (
    workspace_id text NOT NULL,
    journal_id text NOT NULL,
    line_no smallint NOT NULL CHECK (line_no IN (1,2)),
    account_kind text NOT NULL CHECK (account_kind IN ('workspace_receivable','platform_revenue','platform_refund','platform_adjustment')),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    delta_micro bigint NOT NULL,
    PRIMARY KEY(workspace_id,journal_id,line_no),
    FOREIGN KEY(workspace_id,journal_id) REFERENCES commerce.billing_journals(workspace_id,id)
);

ALTER TABLE commerce.billing_journals ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.billing_journals FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.billing_journals
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
ALTER TABLE commerce.billing_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE commerce.billing_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_scope ON commerce.billing_entries
    USING(workspace_id=nullif(current_setting('mender.workspace_id',true),''))
    WITH CHECK(workspace_id=nullif(current_setting('mender.workspace_id',true),''));
CREATE INDEX billing_journal_workspace_time ON commerce.billing_journals(workspace_id,currency,occurred_at,id);
CREATE INDEX billing_journal_basis ON commerce.billing_journals(workspace_id,basis_kind,basis_id,journal_kind);
CREATE UNIQUE INDEX one_usage_charge_journal
  ON commerce.billing_journals(workspace_id,basis_id)
  WHERE journal_kind='charge' AND basis_kind='usage_settlement';

CREATE FUNCTION commerce.guard_billing_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'billing ledger is immutable' USING ERRCODE='42501';
END $$;
CREATE TRIGGER immutable_billing_journals BEFORE UPDATE OR DELETE ON commerce.billing_journals
  FOR EACH ROW EXECUTE FUNCTION commerce.guard_billing_immutable();
CREATE TRIGGER immutable_billing_entries BEFORE UPDATE OR DELETE ON commerce.billing_entries
  FOR EACH ROW EXECUTE FUNCTION commerce.guard_billing_immutable();

CREATE FUNCTION commerce.check_billing_journal_balance() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE w text; j text; k text; d text; c text; a bigint; n integer; total bigint;
DECLARE first_account text; second_account text; first_delta bigint; second_delta bigint; mismatched integer;
BEGIN
    w:=NEW.workspace_id;
    IF TG_TABLE_NAME='billing_journals' THEN j:=NEW.id; ELSE j:=NEW.journal_id; END IF;
    SELECT journal_kind,direction,currency,amount_micro INTO k,d,c,a
      FROM commerce.billing_journals WHERE workspace_id=w AND id=j;
    IF NOT FOUND THEN RAISE EXCEPTION 'billing journal unavailable' USING ERRCODE='23514'; END IF;
    SELECT count(*),coalesce(sum(delta_micro),0),
           max(account_kind) FILTER (WHERE line_no=1),max(account_kind) FILTER (WHERE line_no=2),
           max(delta_micro) FILTER (WHERE line_no=1),max(delta_micro) FILTER (WHERE line_no=2),
           count(*) FILTER (WHERE currency<>c)
      INTO n,total,first_account,second_account,first_delta,second_delta,mismatched
      FROM commerce.billing_entries WHERE workspace_id=w AND journal_id=j;
    IF n<>2 OR total<>0 OR mismatched<>0 THEN
        RAISE EXCEPTION 'billing journal must contain exactly two balanced same-currency entries' USING ERRCODE='23514';
    END IF;
    IF k='charge' AND NOT (d='' AND first_account='workspace_receivable' AND first_delta=a AND second_account='platform_revenue' AND second_delta=-a) THEN
        RAISE EXCEPTION 'invalid charge journal shape' USING ERRCODE='23514';
    ELSIF k='refund' AND NOT (d='credit' AND first_account='workspace_receivable' AND first_delta=-a AND second_account='platform_refund' AND second_delta=a) THEN
        RAISE EXCEPTION 'invalid refund journal shape' USING ERRCODE='23514';
    ELSIF k='adjustment' AND d='debit' AND NOT (first_account='workspace_receivable' AND first_delta=a AND second_account='platform_adjustment' AND second_delta=-a) THEN
        RAISE EXCEPTION 'invalid debit adjustment shape' USING ERRCODE='23514';
    ELSIF k='adjustment' AND d='credit' AND NOT (first_account='workspace_receivable' AND first_delta=-a AND second_account='platform_adjustment' AND second_delta=a) THEN
        RAISE EXCEPTION 'invalid credit adjustment shape' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER billing_journal_balanced
  AFTER INSERT ON commerce.billing_journals DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION commerce.check_billing_journal_balance();
CREATE CONSTRAINT TRIGGER billing_entry_balanced
  AFTER INSERT ON commerce.billing_entries DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION commerce.check_billing_journal_balance();

CREATE FUNCTION commerce.append_usage_charge_journal(
    requested_workspace text,run_value text,currency_value text,charged_value bigint,at_time timestamptz) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE journal_id text; business text; existing record;
BEGIN
    IF requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$' OR run_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR currency_value !~ '^[A-Z]{3}$' OR charged_value<0 OR at_time IS NULL THEN
        RAISE EXCEPTION 'invalid usage billing fact' USING ERRCODE='22023';
    END IF;
    journal_id:='charge_'||md5(requested_workspace||':'||run_value);
    business:='usage:'||run_value;
    SELECT * INTO existing FROM commerce.billing_journals WHERE workspace_id=requested_workspace AND business_key=business;
    IF FOUND THEN
        IF existing.id<>journal_id OR existing.journal_kind<>'charge' OR existing.currency<>currency_value
           OR existing.amount_micro<>charged_value OR existing.basis_kind<>'usage_settlement' OR existing.basis_id<>run_value THEN
            RAISE EXCEPTION 'conflicting usage billing fact' USING ERRCODE='23514';
        END IF;
        RETURN journal_id;
    END IF;
    INSERT INTO commerce.billing_journals(workspace_id,id,business_key,journal_kind,direction,currency,amount_micro,basis_kind,basis_id,reason,occurred_at)
      VALUES(requested_workspace,journal_id,business,'charge','',currency_value,charged_value,'usage_settlement',run_value,'usage settlement',at_time);
    INSERT INTO commerce.billing_entries(workspace_id,journal_id,line_no,account_kind,currency,delta_micro) VALUES
      (requested_workspace,journal_id,1,'workspace_receivable',currency_value,charged_value),
      (requested_workspace,journal_id,2,'platform_revenue',currency_value,-charged_value);
    RETURN journal_id;
END $$;

CREATE FUNCTION commerce.mirror_usage_settlement_to_billing() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM commerce.append_usage_charge_journal(NEW.workspace_id,NEW.run_id,NEW.currency,NEW.charged_micro,NEW.settled_at);
    RETURN NEW;
END $$;
CREATE TRIGGER mirror_usage_settlement_to_billing
  AFTER INSERT ON commerce.usage_settlements
  FOR EACH ROW EXECUTE FUNCTION commerce.mirror_usage_settlement_to_billing();

SELECT commerce.append_usage_charge_journal(workspace_id,run_id,currency,charged_micro,settled_at)
  FROM commerce.usage_settlements;

CREATE FUNCTION governance.request_commerce_approval(
    requested_workspace text,approval text,requester_id text,action_name text,business_key_value text,
    basis_kind_value text,basis_id_value text,direction_value text,amount_value bigint,currency_value text,
    reason_text text,at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parameters jsonb; parameters_digest text; target_type text;
BEGIN
    IF action_name NOT IN ('commerce.refund','commerce.adjustment')
       OR business_key_value !~ '^[A-Za-z0-9_.:-]{1,200}$' OR basis_id_value !~ '^[A-Za-z0-9_-]{1,128}$'
       OR amount_value<=0 OR currency_value !~ '^[A-Z]{3}$' THEN
        RAISE EXCEPTION 'invalid commerce approval request' USING ERRCODE='22023';
    END IF;
    IF action_name='commerce.refund' THEN
        IF basis_kind_value<>'usage_settlement' OR direction_value<>'credit' THEN
            RAISE EXCEPTION 'invalid refund approval binding' USING ERRCODE='22023';
        END IF;
        target_type:='billing_refund';
    ELSE
        IF basis_kind_value NOT IN ('run','incident','reconciliation') OR direction_value NOT IN ('debit','credit') THEN
            RAISE EXCEPTION 'invalid adjustment approval binding' USING ERRCODE='22023';
        END IF;
        target_type:='billing_adjustment';
    END IF;
    parameters:=jsonb_build_object('basis_id',basis_id_value,'basis_kind',basis_kind_value,'business_key',business_key_value,'direction',direction_value);
    parameters_digest:=encode(sha256(convert_to(parameters::text,'UTF8')),'hex');
    PERFORM governance.submit_dangerous_operation(
      requested_workspace,approval,requester_id,'platform_staff',requester_id,
      action_name,target_type,basis_id_value,business_key_value,parameters,parameters_digest,
      amount_value,currency_value,reason_text,at_time,expiry_time);
END $$;

CREATE FUNCTION governance.consume_commerce_approval(
    requested_workspace text,approval text,consumer_id text,action_name text,business_key_value text,
    basis_kind_value text,basis_id_value text,direction_value text,amount_value bigint,currency_value text,at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parameters jsonb; parameters_digest text; target_type text; a record;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    target_type:=CASE WHEN action_name='commerce.refund' THEN 'billing_refund' WHEN action_name='commerce.adjustment' THEN 'billing_adjustment' ELSE '' END;
    parameters:=jsonb_build_object('basis_id',basis_id_value,'basis_kind',basis_kind_value,'business_key',business_key_value,'direction',direction_value);
    parameters_digest:=encode(sha256(convert_to(parameters::text,'UTF8')),'hex');
    SELECT * INTO a FROM governance.dangerous_operation_approvals
     WHERE workspace_id=requested_workspace AND id=approval FOR UPDATE;
    IF NOT FOUND OR a.state<>'approved' OR a.reviewed_at IS NULL OR at_time<a.reviewed_at OR a.expires_at<=at_time
       OR a.requester_user_id<>consumer_id OR a.subject_kind<>'platform_staff' OR a.subject_id<>consumer_id
       OR a.action<>action_name OR a.target_kind<>target_type OR a.target_id<>basis_id_value OR a.target_version<>business_key_value
       OR a.parameters_json<>parameters OR a.parameters_sha256<>parameters_digest
       OR a.amount_micro<>amount_value OR a.currency<>currency_value THEN
        RAISE EXCEPTION 'exact commerce approval unavailable' USING ERRCODE='42501';
    END IF;
    UPDATE governance.dangerous_operation_approvals SET state='consumed',consumed_at=at_time
     WHERE workspace_id=requested_workspace AND id=approval AND state='approved';
    PERFORM governance.append_dangerous_operation_audit(requested_workspace,approval,'consumed',consumer_id,at_time,action_name);
END $$;

CREATE FUNCTION commerce.post_billing_refund(
    requested_workspace text,business_key_value text,run_value text,amount_value bigint,currency_value text,
    approval_value text,actor_id text,reason_text text,at_time timestamptz)
RETURNS TABLE(journal_id text,replay boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE existing record; charged bigint; prior_refunds bigint; new_id text;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT * INTO existing FROM commerce.billing_journals WHERE workspace_id=requested_workspace AND business_key=business_key_value;
    IF FOUND THEN
        IF existing.journal_kind='refund' AND existing.direction='credit' AND existing.currency=currency_value
           AND existing.amount_micro=amount_value AND existing.basis_kind='usage_settlement' AND existing.basis_id=run_value
           AND existing.approval_id=approval_value AND existing.actor_user_id=actor_id AND existing.reason=reason_text THEN
            RETURN QUERY SELECT existing.id,true; RETURN;
        END IF;
        RAISE EXCEPTION 'billing business key conflict' USING ERRCODE='23505';
    END IF;
    IF amount_value<=0 OR char_length(reason_text) NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid refund' USING ERRCODE='22023';
    END IF;
    SELECT charged_micro INTO charged FROM commerce.usage_settlements
      WHERE workspace_id=requested_workspace AND run_id=run_value AND currency=currency_value FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'refundable settlement unavailable' USING ERRCODE='P0002'; END IF;
    SELECT coalesce(sum(amount_micro),0) INTO prior_refunds FROM commerce.billing_journals
      WHERE workspace_id=requested_workspace AND journal_kind='refund' AND basis_kind='usage_settlement' AND basis_id=run_value AND currency=currency_value;
    IF prior_refunds+amount_value>charged THEN RAISE EXCEPTION 'refund exceeds settled charge' USING ERRCODE='23514'; END IF;
    PERFORM governance.consume_commerce_approval(requested_workspace,approval_value,actor_id,'commerce.refund',business_key_value,'usage_settlement',run_value,'credit',amount_value,currency_value,at_time);
    new_id:='refund_'||md5(requested_workspace||':'||business_key_value);
    INSERT INTO commerce.billing_journals(workspace_id,id,business_key,journal_kind,direction,currency,amount_micro,basis_kind,basis_id,approval_id,actor_user_id,reason,occurred_at)
      VALUES(requested_workspace,new_id,business_key_value,'refund','credit',currency_value,amount_value,'usage_settlement',run_value,approval_value,actor_id,reason_text,at_time);
    INSERT INTO commerce.billing_entries(workspace_id,journal_id,line_no,account_kind,currency,delta_micro) VALUES
      (requested_workspace,new_id,1,'workspace_receivable',currency_value,-amount_value),
      (requested_workspace,new_id,2,'platform_refund',currency_value,amount_value);
    RETURN QUERY SELECT new_id,false;
END $$;

CREATE FUNCTION commerce.post_billing_adjustment(
    requested_workspace text,business_key_value text,basis_kind_value text,basis_id_value text,direction_value text,
    amount_value bigint,currency_value text,approval_value text,actor_id text,reason_text text,at_time timestamptz)
RETURNS TABLE(journal_id text,replay boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE existing record; new_id text; workspace_delta bigint;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    SELECT * INTO existing FROM commerce.billing_journals WHERE workspace_id=requested_workspace AND business_key=business_key_value;
    IF FOUND THEN
        IF existing.journal_kind='adjustment' AND existing.direction=direction_value AND existing.currency=currency_value
           AND existing.amount_micro=amount_value AND existing.basis_kind=basis_kind_value AND existing.basis_id=basis_id_value
           AND existing.approval_id=approval_value AND existing.actor_user_id=actor_id AND existing.reason=reason_text THEN
            RETURN QUERY SELECT existing.id,true; RETURN;
        END IF;
        RAISE EXCEPTION 'billing business key conflict' USING ERRCODE='23505';
    END IF;
    IF basis_kind_value NOT IN ('run','incident','reconciliation') OR direction_value NOT IN ('debit','credit')
       OR amount_value<=0 OR char_length(reason_text) NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid billing adjustment' USING ERRCODE='22023';
    END IF;
    IF basis_kind_value='run' AND NOT EXISTS(SELECT 1 FROM execution.runs r WHERE r.workspace_id=requested_workspace AND r.id=basis_id_value) THEN
        RAISE EXCEPTION 'adjustment Run basis unavailable' USING ERRCODE='P0002';
    END IF;
    IF basis_kind_value='incident' AND NOT EXISTS(SELECT 1 FROM governance.platform_incidents i WHERE i.id=basis_id_value) THEN
        RAISE EXCEPTION 'adjustment incident basis unavailable' USING ERRCODE='P0002';
    END IF;
    PERFORM governance.consume_commerce_approval(requested_workspace,approval_value,actor_id,'commerce.adjustment',business_key_value,basis_kind_value,basis_id_value,direction_value,amount_value,currency_value,at_time);
    new_id:='adjust_'||md5(requested_workspace||':'||business_key_value);
    workspace_delta:=CASE WHEN direction_value='debit' THEN amount_value ELSE -amount_value END;
    INSERT INTO commerce.billing_journals(workspace_id,id,business_key,journal_kind,direction,currency,amount_micro,basis_kind,basis_id,approval_id,actor_user_id,reason,occurred_at)
      VALUES(requested_workspace,new_id,business_key_value,'adjustment',direction_value,currency_value,amount_value,basis_kind_value,basis_id_value,approval_value,actor_id,reason_text,at_time);
    INSERT INTO commerce.billing_entries(workspace_id,journal_id,line_no,account_kind,currency,delta_micro) VALUES
      (requested_workspace,new_id,1,'workspace_receivable',currency_value,workspace_delta),
      (requested_workspace,new_id,2,'platform_adjustment',currency_value,-workspace_delta);
    RETURN QUERY SELECT new_id,false;
END $$;

CREATE FUNCTION commerce.billing_summary(requested_workspace text,actor_id text,currency_value text)
RETURNS TABLE(workspace_id text,currency text,charged_micro bigint,refunded_micro bigint,adjustment_debit_micro bigint,adjustment_credit_micro bigint,net_billed_micro bigint,journal_count bigint,latest_journal_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$' OR currency_value !~ '^[A-Z]{3}$' THEN
        RAISE EXCEPTION 'billing audit authority required' USING ERRCODE='42501';
    END IF;
    RETURN QUERY WITH journal_totals AS (
      SELECT coalesce(sum(j.amount_micro) FILTER (WHERE j.journal_kind='charge'),0)::bigint AS charged,
             coalesce(sum(j.amount_micro) FILTER (WHERE j.journal_kind='refund'),0)::bigint AS refunded,
             coalesce(sum(j.amount_micro) FILTER (WHERE j.journal_kind='adjustment' AND j.direction='debit'),0)::bigint AS adjusted_debit,
             coalesce(sum(j.amount_micro) FILTER (WHERE j.journal_kind='adjustment' AND j.direction='credit'),0)::bigint AS adjusted_credit,
             count(*)::bigint AS journals,max(j.occurred_at) AS latest
        FROM commerce.billing_journals j
       WHERE j.workspace_id=requested_workspace AND j.currency=currency_value
    ), receivable AS (
      SELECT coalesce(sum(e.delta_micro),0)::bigint AS net
        FROM commerce.billing_entries e
       WHERE e.workspace_id=requested_workspace AND e.currency=currency_value AND e.account_kind='workspace_receivable'
    )
    SELECT requested_workspace,currency_value,t.charged,t.refunded,t.adjusted_debit,t.adjusted_credit,r.net,t.journals,t.latest
      FROM journal_totals t CROSS JOIN receivable r;
END $$;

CREATE FUNCTION commerce.billing_reconciliation(requested_workspace text,actor_id text,currency_value text)
RETURNS TABLE(workspace_id text,currency text,usage_settlement_charged_micro bigint,ledger_charge_micro bigint,missing_charge_journal_count bigint,pending_reconcile_count bigint,difference_micro bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE usage_total bigint; ledger_total bigint; missing_count bigint; pending_count bigint;
BEGIN
    IF NOT identity.platform_staff_allows(actor_id,'platform:audit') OR requested_workspace !~ '^[A-Za-z0-9_-]{1,128}$' OR currency_value !~ '^[A-Z]{3}$' THEN
        RAISE EXCEPTION 'billing reconciliation authority required' USING ERRCODE='42501';
    END IF;
    SELECT coalesce(sum(s.charged_micro),0),count(*) FILTER (WHERE j.id IS NULL)
      INTO usage_total,missing_count
      FROM commerce.usage_settlements s LEFT JOIN commerce.billing_journals j
        ON j.workspace_id=s.workspace_id AND j.journal_kind='charge' AND j.basis_kind='usage_settlement' AND j.basis_id=s.run_id
      WHERE s.workspace_id=requested_workspace AND s.currency=currency_value;
    SELECT coalesce(sum(j.amount_micro),0) INTO ledger_total FROM commerce.billing_journals j
      WHERE j.workspace_id=requested_workspace AND j.currency=currency_value AND j.journal_kind='charge';
    SELECT count(*) INTO pending_count FROM execution.runs r
      JOIN execution.run_admissions a ON (a.workspace_id,a.run_id)=(r.workspace_id,r.id)
      WHERE r.workspace_id=requested_workspace AND r.state='reconciling' AND a.currency=currency_value;
    RETURN QUERY SELECT requested_workspace,currency_value,usage_total,ledger_total,missing_count,pending_count,ledger_total-usage_total;
END $$;

REVOKE ALL ON commerce.billing_journals,commerce.billing_entries FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.guard_billing_immutable() FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.check_billing_journal_balance() FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.append_usage_charge_journal(text,text,text,bigint,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.mirror_usage_settlement_to_billing() FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.request_commerce_approval(text,text,text,text,text,text,text,text,bigint,text,text,timestamptz,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION governance.consume_commerce_approval(text,text,text,text,text,text,text,text,bigint,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.post_billing_refund(text,text,text,bigint,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.post_billing_adjustment(text,text,text,text,text,bigint,text,text,text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.billing_summary(text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION commerce.billing_reconciliation(text,text,text) FROM PUBLIC;

