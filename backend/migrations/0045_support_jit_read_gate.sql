-- T26 hardening: support-reader never receives direct SELECT on tenant Run
-- tables. Every read is mediated by a SECURITY DEFINER function that proves an
-- active JIT grant for the exact workspace/user/scope at the supplied server
-- time before projecting only safe support metadata.

CREATE FUNCTION governance.list_jit_support_runs(
    requested_workspace text,user_id_value text,at_time timestamptz)
RETURNS TABLE(workspace_id text,id text,state text,version bigint,created_at timestamptz,updated_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE grant_id text;
BEGIN
    PERFORM governance.assert_dangerous_workspace(requested_workspace);
    grant_id:=governance.authorize_jit_support(requested_workspace,user_id_value,'run:read',at_time);
    IF grant_id IS NULL THEN
        RAISE EXCEPTION 'active JIT run read grant required' USING ERRCODE='42501';
    END IF;
    RETURN QUERY
      SELECT r.workspace_id,r.id,r.state,r.version,r.created_at,r.updated_at
      FROM execution.runs r
      WHERE r.workspace_id=requested_workspace
      ORDER BY r.updated_at DESC,r.id DESC
      LIMIT 100;
END $$;

REVOKE ALL ON FUNCTION governance.list_jit_support_runs(text,text,timestamptz) FROM PUBLIC;
