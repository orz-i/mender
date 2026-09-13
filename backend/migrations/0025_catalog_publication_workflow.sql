-- Catalog/Toolset publication workflow.
-- Restricted Human management roles may inspect persisted facts and invoke
-- these reviewed transitions, but never receive direct lifecycle column writes.

CREATE FUNCTION catalog.assert_management_workspace(requested_workspace text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    IF requested_workspace IS NULL
       OR requested_workspace<>nullif(current_setting('mender.workspace_id',true),'') THEN
        RAISE EXCEPTION 'workspace scope mismatch' USING ERRCODE='42501';
    END IF;
END $$;

CREATE FUNCTION catalog.tool_version_publish_issues(requested_workspace text, requested_id text, at_time timestamptz)
RETURNS TABLE(code text,target_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO v FROM catalog.tool_version_management
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id;
    IF NOT FOUND THEN
        RETURN QUERY SELECT 'not_found'::text,requested_id;
        RETURN;
    END IF;
    IF v.state<>'draft' THEN
        RETURN QUERY SELECT 'not_draft'::text,requested_id;
        RETURN;
    END IF;
    IF EXISTS(SELECT 1 FROM catalog.tool_versions tv
              WHERE tv.id=v.tool_version_id OR (tv.tool_id=v.tool_id AND tv.version=v.version)) THEN
        RETURN QUERY SELECT 'version_conflict'::text,requested_id;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM commerce.price_versions p
                  WHERE p.id=v.price_version_id AND p.tool_version_id=v.tool_version_id
                    AND p.active AND p.starts_at<=at_time AND p.ends_at>at_time) THEN
        RETURN QUERY SELECT 'pricing_unavailable'::text,v.price_version_id;
    END IF;
END $$;

CREATE FUNCTION catalog.publish_tool_version(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF EXISTS(SELECT 1 FROM catalog.tool_version_publish_issues(requested_workspace,requested_id,at_time)) THEN
        RAISE EXCEPTION 'ToolVersion publication preflight failed' USING ERRCODE='23514';
    END IF;
    SELECT * INTO v FROM catalog.tool_version_management
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='draft'
      FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'ToolVersion is not a draft' USING ERRCODE='23514';
    END IF;
    INSERT INTO catalog.tool_versions(
      id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,
      input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
    VALUES(v.tool_version_id,v.tool_id,v.version,v.provider_id,v.price_version_id,v.deployment_revision,
      v.title,v.description,v.input_schema,v.output_schema,v.side_effect,v.idempotency,v.mcp_publishable,'published',at_time);
    UPDATE catalog.tool_version_management
      SET state='published',published_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='draft';
END $$;

CREATE FUNCTION catalog.retire_tool_version(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO v FROM catalog.tool_version_management
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='published'
      FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'ToolVersion is not published' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM distribution.toolset_bindings b
              WHERE b.workspace_id=requested_workspace AND b.tool_version_id=requested_id AND b.state='published') THEN
        RAISE EXCEPTION 'ToolVersion is bound by a published Toolset' USING ERRCODE='23514';
    END IF;
    UPDATE catalog.tool_versions SET state='retired' WHERE id=requested_id AND state='published';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'published ToolVersion fact unavailable' USING ERRCODE='23514';
    END IF;
    UPDATE catalog.tool_version_management
      SET state='retired',retired_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND tool_version_id=requested_id AND state='published';
END $$;

CREATE FUNCTION distribution.toolset_publish_issues(requested_workspace text, requested_id text, at_time timestamptz)
RETURNS TABLE(code text,target_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE s record; b record; tv record; price record;
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    SELECT * INTO s FROM distribution.toolsets
      WHERE workspace_id=requested_workspace AND id=requested_id;
    IF NOT FOUND THEN
        RETURN QUERY SELECT 'not_found'::text,requested_id;
        RETURN;
    END IF;
    IF s.state<>'draft' THEN
        RETURN QUERY SELECT 'not_draft'::text,requested_id;
        RETURN;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM distribution.toolset_bindings
                  WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='draft') THEN
        RETURN QUERY SELECT 'empty_toolset'::text,requested_id;
        RETURN;
    END IF;
    FOR b IN SELECT * FROM distribution.toolset_bindings
      WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='draft'
      ORDER BY tool_version_id
    LOOP
        SELECT m.provider_id,m.price_version_id,m.mcp_publishable,m.state,t.state AS runtime_state
          INTO tv
          FROM catalog.tool_version_management m
          LEFT JOIN catalog.tool_versions t ON t.id=m.tool_version_id
          WHERE m.workspace_id=requested_workspace AND m.tool_version_id=b.tool_version_id;
        IF NOT FOUND OR tv.state<>'published' OR tv.runtime_state<>'published' THEN
            RETURN QUERY SELECT 'tool_version_unpublished'::text,b.tool_version_id;
            CONTINUE;
        END IF;
        IF b.connection_id IS NULL OR NOT EXISTS(
            SELECT 1 FROM connections.connections c
             WHERE c.workspace_id=requested_workspace AND c.id=b.connection_id
               AND c.provider_id=tv.provider_id AND c.state='active' AND c.expires_at>at_time) THEN
            RETURN QUERY SELECT 'connection_unavailable'::text,coalesce(b.connection_id,b.tool_version_id);
        END IF;
        SELECT p.currency,p.reserve_micro INTO price FROM commerce.price_versions p
          WHERE p.id=tv.price_version_id AND p.tool_version_id=b.tool_version_id
            AND p.active AND p.starts_at<=at_time AND p.ends_at>at_time;
        IF NOT FOUND THEN
            RETURN QUERY SELECT 'pricing_unavailable'::text,tv.price_version_id;
        ELSE
            IF NOT EXISTS(SELECT 1 FROM commerce.budget_periods bp
                          WHERE bp.workspace_id=requested_workspace AND bp.budget_id=b.budget_id
                            AND bp.currency=price.currency AND bp.active
                            AND bp.starts_at<=at_time AND bp.ends_at>at_time) THEN
                RETURN QUERY SELECT 'budget_unavailable'::text,b.budget_id;
            END IF;
        END IF;
        IF b.mcp_exposed AND NOT tv.mcp_publishable THEN
            RETURN QUERY SELECT 'mcp_contract_unavailable'::text,b.tool_version_id;
        END IF;
    END LOOP;
END $$;

CREATE FUNCTION distribution.publish_toolset(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    IF EXISTS(SELECT 1 FROM distribution.toolset_publish_issues(requested_workspace,requested_id,at_time)) THEN
        RAISE EXCEPTION 'Toolset publication preflight failed' USING ERRCODE='23514';
    END IF;
    PERFORM 1 FROM distribution.toolsets
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='draft' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Toolset is not a draft' USING ERRCODE='23514';
    END IF;
    UPDATE distribution.toolset_bindings
      SET state='published',published_at=at_time
      WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='draft';
    UPDATE distribution.toolsets
      SET state='published',published_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='draft';
END $$;

CREATE FUNCTION distribution.retire_toolset(requested_workspace text, requested_id text, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
    PERFORM catalog.assert_management_workspace(requested_workspace);
    PERFORM 1 FROM distribution.toolsets
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='published' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Toolset is not published' USING ERRCODE='23514';
    END IF;
    UPDATE distribution.toolset_bindings SET state='retired'
      WHERE workspace_id=requested_workspace AND toolset_version_id=requested_id AND state='published';
    UPDATE distribution.toolsets
      SET state='retired',retired_at=at_time,updated_at=at_time
      WHERE workspace_id=requested_workspace AND id=requested_id AND state='published';
END $$;

REVOKE ALL ON FUNCTION catalog.assert_management_workspace(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION catalog.tool_version_publish_issues(text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION catalog.publish_tool_version(text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION catalog.retire_tool_version(text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION distribution.toolset_publish_issues(text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION distribution.publish_toolset(text,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION distribution.retire_toolset(text,text,timestamptz) FROM PUBLIC;
