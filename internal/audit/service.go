// Package audit defines the bounded, content-free access decision record.
package audit

import (
	"context"
	"errors"
	"slices"
	"time"

	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/repository"
)

type Event struct {
	Timestamp                                             time.Time
	RequestID, UserID, TenantID, Tool, Action, ResourceID string
	Decision, Reason, QueryHash                           string
	DurationMS                                            int64
	// DisclosedDocumentIDs is nil when not applicable and empty when nothing was returned.
	DisclosedDocumentIDs []string
}

type Store interface {
	Append(context.Context, Event) error
}

type outcome struct {
	decision, reason string
	disclosed        []string
}
type outcomeKey struct{}

// Track creates request-local state; no caller-controlled metadata is trusted.
func Track(ctx context.Context) context.Context {
	return context.WithValue(ctx, outcomeKey{}, &outcome{})
}

// RecordError preserves internal categories before handlers sanitize client errors.
func RecordError(ctx context.Context, err error) {
	o, _ := ctx.Value(outcomeKey{}).(*outcome)
	if o == nil || err == nil {
		return
	}
	o.decision, o.reason = "ERROR", "service_unavailable"
	switch {
	case errors.Is(err, repository.ErrNotFound):
		o.decision, o.reason = "DENY", "not_found_or_forbidden"
	case errors.Is(err, authn.ErrUnauthenticated):
		o.decision, o.reason = "DENY", "unauthenticated"
	case errors.Is(err, authz.ErrInvalidInput):
		o.decision, o.reason = "DENY", "invalid_arguments"
	}
}

// RecordDisclosed stores the distinct document IDs a successful result returns.
func RecordDisclosed(ctx context.Context, documentIDs []string) {
	o, _ := ctx.Value(outcomeKey{}).(*outcome)
	if o == nil {
		return
	}
	o.disclosed = []string{}
	for _, id := range documentIDs {
		if !slices.Contains(o.disclosed, id) {
			o.disclosed = append(o.disclosed, id)
		}
	}
}

// Disclosed returns nil unless a handler recorded its returned documents.
func Disclosed(ctx context.Context) []string {
	if o, _ := ctx.Value(outcomeKey{}).(*outcome); o != nil && o.disclosed != nil {
		return slices.Clone(o.disclosed)
	}
	return nil
}

func Decision(ctx context.Context, failed bool) (string, string) {
	if o, _ := ctx.Value(outcomeKey{}).(*outcome); o != nil && o.decision != "" {
		return o.decision, o.reason
	}
	if failed {
		return "DENY", "invalid_arguments"
	}
	return "ALLOW", "authorized"
}
