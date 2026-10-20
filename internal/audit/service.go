// Package audit defines the bounded, content-free access decision record.
package audit

import (
	"context"
	"errors"
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
}

type Store interface {
	Append(context.Context, Event) error
}

type outcome struct{ decision, reason string }
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

func Decision(ctx context.Context, failed bool) (string, string) {
	if o, _ := ctx.Value(outcomeKey{}).(*outcome); o != nil && o.decision != "" {
		return o.decision, o.reason
	}
	if failed {
		return "DENY", "invalid_arguments"
	}
	return "ALLOW", "authorized"
}
