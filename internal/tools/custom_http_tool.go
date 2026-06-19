package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CustomHTTPTool wraps a registered HTTP tool definition and executes it via HTTP POST.
// GoClaw injects tenant context headers (X-Goclaw-Tenant-ID, X-Goclaw-User-ID,
// X-Goclaw-Session-Key) into every call so the downstream service can identify the workspace.
type CustomHTTPTool struct {
	def    store.CustomHTTPToolDef
	params map[string]any
	client *http.Client
}

// NewCustomHTTPTool creates a Tool from a stored definition.
func NewCustomHTTPTool(def store.CustomHTTPToolDef) *CustomHTTPTool {
	timeout := time.Duration(def.Config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	var params map[string]any
	if len(def.Parameters) > 0 {
		_ = json.Unmarshal(def.Parameters, &params) // best-effort; nil params is fine
	}
	return &CustomHTTPTool{
		def:    def,
		params: params,
		client: &http.Client{Timeout: timeout},
	}
}

func (t *CustomHTTPTool) Name() string              { return t.def.Name }
func (t *CustomHTTPTool) Description() string       { return t.def.Description }
func (t *CustomHTTPTool) Parameters() map[string]any { return t.params }

// Execute calls the remote HTTP endpoint with the tool arguments and injected tenant headers.
func (t *CustomHTTPTool) Execute(ctx context.Context, args map[string]any) *Result {
	body, err := json.Marshal(args)
	if err != nil {
		return NewResult(fmt.Sprintf("custom_http_tool %s: marshal args: %v", t.def.Name, err))
	}

	method := t.def.Config.Method
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, t.def.Config.URL, bytes.NewReader(body))
	if err != nil {
		return NewResult(fmt.Sprintf("custom_http_tool %s: build request: %v", t.def.Name, err))
	}
	req.Header.Set("Content-Type", "application/json")

	// Inject configured headers (secrets etc.)
	for k, v := range t.def.Config.Headers {
		req.Header.Set(k, v)
	}

	// Inject tenant context from the Go context (set by gateway before pipeline runs).
	if v := httpToolTenantIDFromCtx(ctx); v != "" {
		req.Header.Set("X-Goclaw-Tenant-ID", v)
	}
	if v := httpToolUserIDFromCtx(ctx); v != "" {
		req.Header.Set("X-Goclaw-User-ID", v)
	}
	if v := httpToolSessionKeyFromCtx(ctx); v != "" {
		req.Header.Set("X-Goclaw-Session-Key", v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return NewResult(fmt.Sprintf("custom_http_tool %s: http: %v", t.def.Name, err))
	}
	defer resp.Body.Close()

	result, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max
	if err != nil {
		return NewResult(fmt.Sprintf("custom_http_tool %s: read response: %v", t.def.Name, err))
	}
	if resp.StatusCode >= 400 {
		return NewResult(fmt.Sprintf("custom_http_tool %s: status %d: %s", t.def.Name, resp.StatusCode, result))
	}
	return NewResult(string(result))
}

// Context key type to avoid collision with other packages.
type httpToolCtxKey string

const (
	httpToolCtxTenantID   httpToolCtxKey = "goclaw_http_tool_tenant_id"
	httpToolCtxUserID     httpToolCtxKey = "goclaw_http_tool_user_id"
	httpToolCtxSessionKey httpToolCtxKey = "goclaw_http_tool_session_key"
)

// WithHTTPToolContext injects tenant/user/session identifiers into the context
// for downstream custom HTTP tool calls. Call this in the gateway before running the pipeline.
func WithHTTPToolContext(ctx context.Context, tenantID, userID, sessionKey string) context.Context {
	ctx = context.WithValue(ctx, httpToolCtxTenantID, tenantID)
	ctx = context.WithValue(ctx, httpToolCtxUserID, userID)
	ctx = context.WithValue(ctx, httpToolCtxSessionKey, sessionKey)
	return ctx
}

func httpToolTenantIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(httpToolCtxTenantID).(string)
	return v
}

func httpToolUserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(httpToolCtxUserID).(string)
	return v
}

func httpToolSessionKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(httpToolCtxSessionKey).(string)
	return v
}
