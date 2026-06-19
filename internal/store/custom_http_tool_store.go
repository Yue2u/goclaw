package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CustomHTTPToolDef defines an agent-level HTTP tool that GoClaw calls when the LLM invokes it.
// GoClaw injects tenant context headers into each call automatically.
type CustomHTTPToolDef struct {
	ID          uuid.UUID       `json:"id"       db:"id"`
	AgentID     *uuid.UUID      `json:"agent_id" db:"agent_id"` // nil = tenant-wide
	TenantID    uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	Name        string          `json:"name"        db:"name"`
	Description string          `json:"description" db:"description"`
	Parameters  json.RawMessage `json:"parameters"  db:"parameters"` // JSON Schema
	Config      HTTPToolConfig  `json:"config"      db:"-"`
	ConfigRaw   json.RawMessage `json:"-"           db:"config"`
	CreatedAt   time.Time       `json:"created_at"  db:"created_at"`
}

// HTTPToolConfig holds the HTTP call configuration for a custom tool.
type HTTPToolConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout int               `json:"timeout"` // seconds
}

// CustomHTTPToolStore persists custom HTTP tool definitions per tenant/agent.
type CustomHTTPToolStore interface {
	CreateTool(ctx context.Context, def *CustomHTTPToolDef) error
	ListTools(ctx context.Context, tenantID uuid.UUID, agentID *uuid.UUID) ([]CustomHTTPToolDef, error)
	DeleteTool(ctx context.Context, id uuid.UUID) error
}
