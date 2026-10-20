package tools

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/repository"
)

type SourcesInput struct {
	Limit *int `json:"limit,omitempty"`
}
type SourcesOutput struct {
	Sources []domain.Source `json:"sources"`
}
type DocumentInput struct {
	DocumentID string `json:"document_id"`
	MaxChars   *int   `json:"max_chars,omitempty"`
}

func principal(req *mcp.CallToolRequest) *authn.Principal {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return nil
	}
	p, _ := req.Extra.TokenInfo.Extra[PrincipalKey].(*authn.Principal)
	return p
}
func toolError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return errors.New("resource not found")
	case errors.Is(err, authn.ErrUnauthenticated):
		return errors.New("unauthenticated")
	case errors.Is(err, authz.ErrInvalidInput):
		return errors.New("invalid resource arguments")
	default:
		return errors.New("resource service unavailable")
	}
}
func RegisterDocuments(s *mcp.Server, service *authz.Service, logger *slog.Logger) {
	logResult := func(ctx context.Context, req *mcp.CallToolRequest, name string, start time.Time, err error) {
		decision := "ALLOW"
		if err != nil {
			decision = "ERROR"
			if errors.Is(err, repository.ErrNotFound) || errors.Is(err, authn.ErrUnauthenticated) {
				decision = "DENY"
			}
		}
		attrs := []any{"tool", name, "decision", decision, "duration", time.Since(start)}
		if req.Extra != nil {
			attrs = append(attrs, "request_id", req.Extra.Header.Get("X-Request-ID"))
		}
		if p := principal(req); p != nil {
			attrs = append(attrs, "user_id", p.UserID, "tenant_id", p.TenantID)
		}
		logger.InfoContext(ctx, "resource access", attrs...)
	}
	mcp.AddTool(s, &mcp.Tool{Name: "list_accessible_sources", Description: "List only sources readable by the authenticated caller. Limit defaults to 20 (1–100).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, "additionalProperties": false}},
		func(ctx context.Context, req *mcp.CallToolRequest, in SourcesInput) (*mcp.CallToolResult, SourcesOutput, error) {
			start := time.Now()
			limit := 20
			if in.Limit != nil {
				limit = *in.Limit
			}
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			sources, err := service.ListAccessibleSources(ctx, principal(req), limit)
			logResult(ctx, req, "list_accessible_sources", start, err)
			if err != nil {
				return nil, SourcesOutput{}, toolError(err)
			}
			if sources == nil {
				sources = []domain.Source{}
			}
			return nil, SourcesOutput{Sources: sources}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_document", Description: "Read a document with a fresh permission check. max_chars defaults to 12000 (1–50000 Unicode characters).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"document_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 36},
			"max_chars":   map[string]any{"type": "integer", "minimum": 1, "maximum": 50000}}, "required": []string{"document_id"}, "additionalProperties": false}},
		func(ctx context.Context, req *mcp.CallToolRequest, in DocumentInput) (*mcp.CallToolResult, *domain.Document, error) {
			start := time.Now()
			maxChars := 12000
			if in.MaxChars != nil {
				maxChars = *in.MaxChars
			}
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			doc, err := service.GetDocument(ctx, principal(req), in.DocumentID, maxChars)
			logResult(ctx, req, "get_document", start, err)
			if err != nil {
				return nil, nil, toolError(err)
			}
			return nil, doc, nil
		})
}
