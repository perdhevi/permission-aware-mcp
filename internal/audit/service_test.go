package audit

import (
	"context"
	"slices"
	"testing"
)

func TestDisclosed(t *testing.T) {
	if Disclosed(Track(context.Background())) != nil {
		t.Fatal("unrecorded disclosures must be nil (stored as NULL)")
	}
	ctx := Track(context.Background())
	RecordDisclosed(ctx, nil)
	if got := Disclosed(ctx); got == nil || len(got) != 0 {
		t.Fatalf("an empty result must be recorded as empty, not nil: %#v", got)
	}
	ctx = Track(context.Background())
	RecordDisclosed(ctx, []string{"b", "a", "b", "a", "c"})
	got := Disclosed(ctx)
	if !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("want distinct IDs in result order: %v", got)
	}
	got[0] = "mutated"
	if Disclosed(ctx)[0] != "b" {
		t.Fatal("callers must receive a copy")
	}
	RecordDisclosed(context.Background(), []string{"untracked"}) // must not panic
}
