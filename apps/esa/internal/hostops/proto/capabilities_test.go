package proto

import (
	"slices"
	"testing"
)

func TestLegacyActionsAreStillActions(t *testing.T) {
	for _, a := range LegacyActions {
		if !slices.Contains(Actions, a) {
			t.Errorf("legacy action %q was removed; old helpers would be told it exists", a)
		}
	}
}

func TestSupports(t *testing.T) {
	if !Supports(nil, "status") || Supports(nil, "inventory") {
		t.Fatal("a helper without a list has the legacy actions only")
	}
	if !Supports([]string{}, "version") {
		t.Fatal("version is always allowed")
	}
	if Supports([]string{"status"}, "clean") {
		t.Fatal("an action the helper did not list was allowed")
	}
	if got := Missing(nil); !slices.Equal(got, []string{"recovery", "inventory", FeatureUpgradeAllow}) {
		t.Fatalf("missing for a legacy helper: %v", got)
	}
	if got := Missing(Capabilities()); len(got) != 0 {
		t.Fatalf("a current helper misses %v", got)
	}
}
