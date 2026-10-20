package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"permission-aware-mcp/internal/audit"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/mcpserver/tools"
)

// Wrap outside SDK argument validation so rejected identity spoofing is audited too.
func auditMiddleware(store audit.Store, logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			req, ok := request.(*mcp.CallToolRequest)
			if method != "tools/call" || !ok {
				return next(ctx, method, request)
			}
			start := time.Now()
			ctx = audit.Track(ctx)
			result, err := next(ctx, method, request)
			failed := err != nil
			if r, ok := result.(*mcp.CallToolResult); ok && r != nil {
				failed = failed || r.IsError
			}
			e := audit.Event{Timestamp: start.UTC(), DurationMS: time.Since(start).Milliseconds(), Tool: req.Params.Name, Action: "read"}
			e.Decision, e.Reason = audit.Decision(ctx, failed)
			switch e.Tool {
			case "whoami":
				e.Action = "identity"
			case "get_document", "list_accessible_sources", "search_knowledge":
			default:
				e.Tool, e.Action, e.Decision, e.Reason = "unknown", "unknown", "DENY", "unknown_tool"
			}
			if req.Extra != nil && req.Extra.TokenInfo != nil {
				e.RequestID = req.Extra.Header.Get("X-Request-ID")
				if p, _ := req.Extra.TokenInfo.Extra[tools.PrincipalKey].(*authn.Principal); p != nil {
					e.UserID, e.TenantID = p.UserID, p.TenantID
				}
			}
			// Decode only resource/query fields; never persist raw arguments or errors.
			var args map[string]json.RawMessage
			if json.Unmarshal(req.Params.Arguments, &args) == nil {
				var value string
				if e.Tool == "search_knowledge" && json.Unmarshal(args["query"], &value) == nil {
					h := sha256.Sum256([]byte(value))
					e.QueryHash = hex.EncodeToString(h[:])
				}
				if e.Tool == "get_document" && json.Unmarshal(args["document_id"], &value) == nil {
					var id pgtype.UUID
					if id.Scan(value) == nil && id.Valid {
						e.ResourceID = value
					}
				}
			}
			// Cancellation of the read must not cancel its audit record.
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			writeErr := errors.New("audit store is required")
			if store != nil {
				writeErr = store.Append(writeCtx, e)
			}
			if writeErr != nil {
				logger.ErrorContext(writeCtx, "audit storage unavailable", "request_id", e.RequestID, "tool", e.Tool)
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "resource service unavailable"}}}, nil
			}
			logger.InfoContext(writeCtx, "audited access", "request_id", e.RequestID, "tool", e.Tool, "user_id", e.UserID, "tenant_id", e.TenantID, "decision", e.Decision, "reason", e.Reason, "duration_ms", e.DurationMS)
			return result, err
		}
	}
}
