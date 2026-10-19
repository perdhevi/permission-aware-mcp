package tools

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"permission-aware-mcp/internal/authn"
)

const PrincipalKey = "permission-aware-mcp/principal"

type WhoamiOutput struct {
	UserID  string   `json:"user_id"`
	Subject string   `json:"subject"`
	Tenant  string   `json:"tenant"`
	Scopes  []string `json:"scopes"`
}

func RegisterWhoami(s *mcp.Server, logger *slog.Logger) {
	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Return the authenticated database identity. No arguments accepted.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, WhoamiOutput, error) {
			start := time.Now()
			if req.Extra == nil || req.Extra.TokenInfo == nil {
				return nil, WhoamiOutput{}, errors.New("unauthenticated")
			}
			p, ok := req.Extra.TokenInfo.Extra[PrincipalKey].(*authn.Principal)
			if !ok || p == nil {
				return nil, WhoamiOutput{}, errors.New("unauthenticated")
			}
			logger.InfoContext(ctx, "tool completed", "request_id", req.Extra.Header.Get("X-Request-ID"), "tool", "whoami", "user_id", p.UserID, "tenant_id", p.TenantID, "duration", time.Since(start))
			return nil, WhoamiOutput{UserID: p.UserID, Subject: p.Subject, Tenant: p.Tenant, Scopes: append([]string{}, p.Scopes...)}, nil
		})
}
