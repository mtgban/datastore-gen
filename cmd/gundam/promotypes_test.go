package main

import (
	"slices"
	"testing"
)

// TestNamedPromotions pins that the sets a release event leads with come off
// it, one code, two by slash or a range of them, and that a code further in
// stays.
func TestNamedPromotions(t *testing.T) {
	cases := map[string][]string{
		"st09 release event":           {"release event"},
		"st10 / eb01 release event":    {"release event"},
		"st11-st14 release event":      {"release event"},
		"evx07 resource set":           {"resource set"},
		"championship-gd01":            {"championship-gd01"},
		"store tournament winner pack": {"store tournament", "winner pack"},
	}
	for tag, want := range cases {
		if got := namedPromotions(tag); !slices.Equal(got, want) {
			t.Errorf("namedPromotions(%q) = %q, want %q", tag, got, want)
		}
	}
}
