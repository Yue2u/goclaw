package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CustomHTTPToolsHandler handles CRUD for tenant-level custom HTTP tools.
// Tools registered here are injected into agent runs and called via HTTP with tenant context headers.
type CustomHTTPToolsHandler struct {
	tools store.CustomHTTPToolStore
}

// NewCustomHTTPToolsHandler creates a handler backed by the given store.
func NewCustomHTTPToolsHandler(s store.CustomHTTPToolStore) *CustomHTTPToolsHandler {
	return &CustomHTTPToolsHandler{tools: s}
}

// RegisterRoutes registers the custom HTTP tools API endpoints.
func (h *CustomHTTPToolsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/tools/custom", requireAuth(permissions.RoleOperator, h.handleCreate))
	mux.HandleFunc("GET /v1/tools/custom", requireAuth(permissions.RoleOperator, h.handleList))
	mux.HandleFunc("DELETE /v1/tools/custom/{id}", requireAuth(permissions.RoleOperator, h.handleDelete))
}

func (h *CustomHTTPToolsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body store.CustomHTTPToolDef
	if !bindJSON(w, r, "", &body) {
		return
	}

	tenantID := store.TenantIDFromContext(r.Context())
	body.TenantID = tenantID

	// Allow optional agent_id scoping from query param
	if agentStr := r.URL.Query().Get("agent_id"); agentStr != "" {
		agentID, err := uuid.Parse(agentStr)
		if err != nil {
			http.Error(w, "invalid agent_id", http.StatusBadRequest)
			return
		}
		body.AgentID = &agentID
	}

	if err := h.tools.CreateTool(r.Context(), &body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *CustomHTTPToolsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())

	var agentID *uuid.UUID
	if agentStr := r.URL.Query().Get("agent_id"); agentStr != "" {
		id, err := uuid.Parse(agentStr)
		if err != nil {
			http.Error(w, "invalid agent_id", http.StatusBadRequest)
			return
		}
		agentID = &id
	}

	defs, err := h.tools.ListTools(r.Context(), tenantID, agentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tools": defs})
}

func (h *CustomHTTPToolsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.tools.DeleteTool(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
