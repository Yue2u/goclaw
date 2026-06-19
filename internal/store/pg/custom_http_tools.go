package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var _ store.CustomHTTPToolStore = (*PGCustomHTTPToolStore)(nil)

// PGCustomHTTPToolStore implements store.CustomHTTPToolStore using PostgreSQL.
type PGCustomHTTPToolStore struct {
	db *sql.DB
}

// NewPGCustomHTTPToolStore creates a new PostgreSQL-backed custom HTTP tool store.
func NewPGCustomHTTPToolStore(db *sql.DB) *PGCustomHTTPToolStore {
	return &PGCustomHTTPToolStore{db: db}
}

// CreateTool inserts a new custom HTTP tool definition.
func (s *PGCustomHTTPToolStore) CreateTool(ctx context.Context, def *store.CustomHTTPToolDef) error {
	def.ID = uuid.New()
	def.CreatedAt = time.Now()

	cfgJSON, err := json.Marshal(def.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO custom_http_tools (id, agent_id, tenant_id, name, description, parameters, config, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		def.ID, def.AgentID, def.TenantID, def.Name, def.Description, def.Parameters, cfgJSON, def.CreatedAt,
	)
	return err
}

// ListTools returns all custom HTTP tools for the given tenant, optionally filtered by agent.
// If agentID is nil, returns tenant-level tools. If agentID is set, returns that agent's tools.
func (s *PGCustomHTTPToolStore) ListTools(ctx context.Context, tenantID uuid.UUID, agentID *uuid.UUID) ([]store.CustomHTTPToolDef, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if agentID != nil {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, agent_id, tenant_id, name, description, parameters, config, created_at
			FROM custom_http_tools
			WHERE tenant_id = $1 AND (agent_id = $2 OR agent_id IS NULL)
			ORDER BY created_at`,
			tenantID, *agentID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, agent_id, tenant_id, name, description, parameters, config, created_at
			FROM custom_http_tools
			WHERE tenant_id = $1 AND agent_id IS NULL
			ORDER BY created_at`,
			tenantID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []store.CustomHTTPToolDef
	for rows.Next() {
		var d store.CustomHTTPToolDef
		if err := rows.Scan(&d.ID, &d.AgentID, &d.TenantID, &d.Name, &d.Description, &d.Parameters, &d.ConfigRaw, &d.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(d.ConfigRaw, &d.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config for tool %s: %w", d.Name, err)
		}
		defs = append(defs, d)
	}
	return defs, rows.Err()
}

// DeleteTool removes a custom HTTP tool by ID.
func (s *PGCustomHTTPToolStore) DeleteTool(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM custom_http_tools WHERE id = $1`, id)
	return err
}
