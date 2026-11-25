package catalog

import (
	"context"
	"fmt"
	"testing"
)

func TestRulePreviewSnapshotDeduplicatesAndBoundsCurrentMemberships(t *testing.T) {
	store, _ := openTest(t)
	for i := 0; i < MaxRulePreviewImages+2; i++ {
		image := candidate(fmt.Sprintf("image-%03d", i))
		if _, err := store.EnsureImage(t.Context(), "wallhaven", "first", "current", "", image); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := store.EnsureImage(t.Context(), "wallhaven", "second", "current", "", image); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, more, err := store.RulePreviewCandidates(t.Context(), []InputRevision{{"wallhaven", "first", "current"}, {"wallhaven", "second", "current"}})
	if err != nil || !more || len(rows) != MaxRulePreviewImages {
		t.Fatalf("bounded snapshot=%d more=%v %v", len(rows), more, err)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Identity.ImageID <= rows[i-1].Identity.ImageID {
			t.Fatal("snapshot duplicated images or lost stable ordering")
		}
	}
	rows, more, err = store.RulePreviewCandidates(t.Context(), []InputRevision{{"wallhaven", "first", "stale"}})
	if err != nil || more || len(rows) != 0 {
		t.Fatalf("stale membership leaked=%+v %v", rows, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := store.RulePreviewCandidates(ctx, []InputRevision{{"wallhaven", "first", "current"}}); err != context.Canceled {
		t.Fatalf("snapshot ignored cancellation=%v", err)
	}
}
