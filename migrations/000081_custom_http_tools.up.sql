CREATE TABLE IF NOT EXISTS custom_http_tools (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL,
    agent_id    UUID        NULL,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    parameters  JSONB       NOT NULL DEFAULT '{}',
    config      JSONB       NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, agent_id, name)
);

CREATE INDEX IF NOT EXISTS idx_custom_http_tools_tenant ON custom_http_tools (tenant_id);
CREATE INDEX IF NOT EXISTS idx_custom_http_tools_agent  ON custom_http_tools (agent_id);
