package rag

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/domain"
)

func TestHashAndChunk(t *testing.T) {
	h := HashEmbedder{}
	ctx := context.Background()
	q, err := h.EmbedQuery(ctx, "Deployment automation")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := h.EmbedDocuments(ctx, []string{"deployment AUTOMATION"})
	if err != nil || !reflect.DeepEqual(q, docs[0]) {
		t.Fatal("inconsistent query/document vectors")
	}
	if _, err := VectorLiteral(q, Dimensions); err != nil {
		t.Fatal(err)
	}
	for _, v := range [][]float32{nil, make([]float32, Dimensions), {float32(math.NaN())}, {float32(math.Inf(1))}} {
		if _, err := VectorLiteral(v, len(v)); err == nil {
			t.Fatal("accepted invalid vector")
		}
	}
	if _, err := h.EmbedQuery(ctx, "!!!"); err == nil {
		t.Fatal("accepted empty terms")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.EmbedQuery(canceled, "hello"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	text := strings.Repeat("🦊é界", 800)
	chunks := Chunk(text)
	if len(chunks) != 3 {
		t.Fatalf("chunks: %d", len(chunks))
	}
	for _, part := range chunks {
		if !utf8.ValidString(part) || utf8.RuneCountInString(part) > 1000 {
			t.Fatal("bad chunk")
		}
	}
	rebuilt := chunks[0]
	for _, part := range chunks[1:] {
		rebuilt += string([]rune(part)[100:])
	}
	if rebuilt != text {
		t.Fatal("lost source text")
	}
	if len(Chunk("  \n")) != 0 || len(Chunk(strings.Repeat("a", 1000))) != 1 {
		t.Fatal("empty/exact chunk boundary")
	}
}

type searchSpy struct{ calls int }

func (s *searchSpy) SearchReadable(_ context.Context, tenant, user, vector, model string, dimensions, limit int) ([]domain.SearchResult, error) {
	s.calls++
	if tenant != "00000000-0000-0000-0000-000000000002" || user != "00000000-0000-0000-0000-000000000001" || model != Model || dimensions != Dimensions || limit != 5 {
		return nil, errors.New("untrusted parameters")
	}
	return []domain.SearchResult{}, nil
}
func TestRetrieverBoundary(t *testing.T) {
	spy := &searchSpy{}
	r := NewRetriever(spy, HashEmbedder{})
	ctx := context.Background()
	p := &authn.Principal{UserID: "00000000-0000-0000-0000-000000000001", TenantID: "00000000-0000-0000-0000-000000000002", Roles: []string{"executive"}}
	for _, tc := range []struct {
		p     *authn.Principal
		query string
		limit int
	}{
		{nil, "hello", 5}, {&authn.Principal{}, "hello", 5}, {p, " ", 5}, {p, strings.Repeat("界", 4001), 5}, {p, "hello", 0}, {p, "hello", 21}, {p, "\xff", 5},
	} {
		if _, err := r.Search(ctx, tc.p, tc.query, tc.limit); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
	if spy.calls != 0 {
		t.Fatal("invalid input reached repository")
	}
	for i := 0; i < 2; i++ {
		if _, err := r.Search(ctx, p, "deployment", 5); err != nil {
			t.Fatal(err)
		}
	}
	if spy.calls != 2 {
		t.Fatal("cached authorization")
	}
}
