package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"permission-aware-mcp/internal/authn"
)

const PrincipalKey = "permission-aware-mcp/principal"

type WhoamiOutput struct {
	UserID  string   `json:"user_id"`
	Subject string   `json:"subject"`
	Tenant  string   `json:"tenant"`
	Scopes  []string `json:"scopes"`
	Roles   []string `json:"roles"`
}

func RegisterWhoami(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Return the authenticated database identity. No arguments accepted.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, WhoamiOutput, error) {
			if req.Extra == nil || req.Extra.TokenInfo == nil {
				return nil, WhoamiOutput{}, toolError(ctx, authn.ErrUnauthenticated)
			}
			p, ok := req.Extra.TokenInfo.Extra[PrincipalKey].(*authn.Principal)
			if !ok || p == nil {
				return nil, WhoamiOutput{}, toolError(ctx, authn.ErrUnauthenticated)
			}
			return nil, WhoamiOutput{UserID: p.UserID, Subject: p.Subject, Tenant: p.Tenant, Scopes: append([]string{}, p.Scopes...), Roles: append([]string{}, p.Roles...)}, nil
		})
}
