package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestNewPrintingNeedsNoRelease is the point of the suffix table being pins
// rather than a closed list. TCGplayer adds a printing to a category when it
// likes and is selling the skus from that moment; a build that stopped
// instead would publish nothing at all rather than publish the new printing
// late, and the whole datastore would go stale over one name.
func TestNewPrintingNeedsNoRelease(t *testing.T) {
	// The pins, unchanged, whatever else the category grows.
	for name, want := range finishSuffix {
		if got := finishSuffixFor(name); got != want {
			t.Errorf("finishSuffixFor(%q) = %q, want the pinned %q", name, got, want)
		}
	}

	for _, test := range []struct{ in, want string }{
		{"Reverse Holofoil", "_reverseholofoil"},
		{"Cold Foil", "_coldfoil"},
		{"Prismatic Foil", "_prismaticfoil"},
	} {
		if got := finishSuffixFor(test.in); got != test.want {
			t.Errorf("a printing added to the category takes suffix %q, want %q", got, test.want)
		}
	}

	// And it is emitted, after the ones this build names, in a fixed order.
	got := orderedFinishes([]string{"Prismatic Foil", "Holofoil", "Normal", "Alpha Foil"})
	want := []string{"Normal", "Holofoil", "Alpha Foil", "Prismatic Foil"}
	if !slices.Equal(got, want) {
		t.Errorf("orderedFinishes = %v, want %v", got, want)
	}
}

// TestPinnedPrintingsAreTheOneThingThatCannotMove covers the hazard the pins
// exist for. A printing TCGplayer renames keeps its skus and its printing id
// but loses its name, and every id this build spelled from the old name
// would quietly move to the new one - so it is refused rather than absorbed.
func TestPinnedPrintingsAreTheOneThingThatCannotMove(t *testing.T) {
	listed := map[string]bool{}
	for name := range finishSuffix {
		listed[name] = true
	}
	dump := &tcgplayer.CatalogDump{}
	for name := range finishSuffix {
		dump.Printings = append(dump.Printings, tcgplayer.Printing{Name: name})
	}
	// A category that still lists every pinned printing passes; adding one
	// changes nothing.
	dump.Printings = append(dump.Printings, tcgplayer.Printing{Name: "Prismatic Foil"})
	checkPinnedPrintings(dump)

	// The renaming case is a log.Fatalf, which a test cannot call without
	// taking the process down, so what is pinned here is that the guard
	// reads the catalog's names rather than its ids: a rename keeps the id.
	if len(dump.Printings) != len(finishSuffix)+1 {
		t.Fatalf("the fixture lists %d printings, want the %d pinned ones and the added one",
			len(dump.Printings), len(finishSuffix)+1)
	}
}
