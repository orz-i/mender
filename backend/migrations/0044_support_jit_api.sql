-- T26 support JIT request wrapper. The browser never provides an approval
-- digest or target version; the application adapter canonicalizes the bounded
-- parameter object and this wrapper constructs the persisted JSON facts.

CREATE FUNCTION governance.request_support_jit_approval(
    requested_workspace text,approval text,requester_id text,requested_scopes text[],ttl_seconds integer,
    reason_text text,at_time timestamptz,expiry_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parameters jsonb; parameters_digest text;
BEGIN
    parameters:=jsonb_build_object('scopes',to_jsonb(requested_scopes),'ttl_seconds',ttl_seconds);
    IF NOT governance.valid_support_parameters(parameters) THEN
        RAISE EXCEPTION 'invalid support JIT parameters' USING ERRCODE='22023';
    END IF;
    parameters_digest:=encode(sha256(convert_to(parameters::text,'UTF8')),'hex');
    PERFORM governance.submit_dangerous_operation(
      requested_workspace,approval,requester_id,'platform_staff',requester_id,
      'support.workspace_read','workspace',requested_workspace,'',parameters,parameters_digest,
      NULL,NULL,reason_text,at_time,expiry_time);
END $$;

REVOKE ALL ON FUNCTION governance.request_support_jit_approval(text,text,text,text[],integer,text,timestamptz,timestamptz) FROM PUBLIC;
