-- 0017 intentionally introduced direct-MCP publication fields with fail-closed
-- defaults. Keep that migration immutable. This follow-up preserves legacy
-- non-MCP ToolVersion inserts while requiring a real title for opt-in direct
-- publication.

ALTER TABLE catalog.tool_versions DROP CONSTRAINT tool_versions_title_check;
ALTER TABLE catalog.tool_versions ADD CONSTRAINT tool_versions_title_check
    CHECK (NOT mcp_publishable OR length(title) BETWEEN 1 AND 200);

