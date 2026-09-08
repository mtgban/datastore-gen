package main

import (
	"testing"
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

	// Ordering is printingNames' job here: it ranks the printings this
	// build names first, in the order it names them, and any TCGplayer has
	// added after them by name.
}
