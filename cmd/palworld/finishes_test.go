package main

import (
	"testing"

	"slices"
)

// TestNewPrintingNeedsNoRelease is the point of the suffix table being pins
// rather than a closed list. TCGplayer adds a printing to a category when it
// likes and is selling the skus from that moment; a build that stopped
// instead would publish nothing at all rather than publish the new printing
// late, and the whole datastore would go stale over one name.
//
// Every builder carries its own copy of these helpers, the way this
// repository duplicates every helper it shares, so every builder pins them.
func TestNewPrintingNeedsNoRelease(t *testing.T) {
	// The pins are unchanged, whatever else the category grows: they are
	// the suffixes already in circulation.
	for name, want := range finishSuffix {
		if got := finishSuffixFor(name); got != want {
			t.Errorf("finishSuffixFor(%q) = %q, want the pinned %q", name, got, want)
		}
	}

	for _, test := range []struct{ in, want string }{
		{"Prismatic Foil", "_prismaticfoil"},
		{"Cold Foil", "_coldfoil"},
	} {
		if _, pinned := finishSuffix[test.in]; pinned {
			continue
		}
		if got := finishSuffixFor(test.in); got != test.want {
			t.Errorf("a printing added to the category takes suffix %q, want %q", got, test.want)
		}
	}

	// And it is emitted in the order the catalog displays its printings,
	// with the name settling a tie.
	rank := map[string]int{"Normal": 1, "Holofoil": 2, "Alpha Foil": 2, "Prismatic Foil": 9}
	got := orderedFinishes([]string{"Prismatic Foil", "Holofoil", "Alpha Foil", "Normal"}, rank)
	want := []string{"Normal", "Alpha Foil", "Holofoil", "Prismatic Foil"}
	if !slices.Equal(got, want) {
		t.Errorf("orderedFinishes = %v, want %v", got, want)
	}
}
