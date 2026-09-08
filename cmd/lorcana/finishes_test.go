package main

import (
	"slices"
	"testing"
)

// The uuids this build publishes are spelled from a finish name, and
// go-mtgban's mtgmatcher spells the same names for the datastores that carry
// none. Neither repository imports the other - this one is standalone by
// design - so the agreement is held by these tables rather than by the
// compiler. A change here that is not matched there moves identity that
// lives outside this repository, silently, since a uuid nobody stored
// resolves to nothing rather than erroring.

// TestCanonicalFinish pins the crossing between TCGplayer's vocabulary and
// the matcher's. The names on the left are what the catalog prices a sku
// under; the names on the right are what a uuid carries.
func TestCanonicalFinish(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		// LorcanaJSON's placeholder for a plain printing
		{"None", finishNonfoil},
		// TCGplayer's whole vocabulary for category 71
		{"Normal", finishNonfoil},
		{"Cold Foil", finishFoil},
		{"Holofoil", finishHolofoil},
		// However a source writes them
		{"cold foil", finishFoil},
		{"COLD-FOIL", finishFoil},
		{"normal", finishNonfoil},
		// A foil type upstream names and the catalog does not: handed
		// back normalized, because the vocabulary is data
		{"RainbowPillars", "rainbowpillars"},
		{"FreeForm1", "freeform1"},
		{"Silver", "silver"},
		{"", ""},
	} {
		if got := canonicalFinish(test.in); got != test.want {
			t.Errorf("canonicalFinish(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestPrintingUUID pins the uuid a printing prices. mtgmatcher/lorcana
// spells these itself for a datastore published before printingIds existed,
// so the two spellings have to agree exactly.
func TestPrintingUUID(t *testing.T) {
	for _, test := range []struct {
		id     int
		finish string
		want   string
	}{
		{1951, finishNonfoil, "1951"},
		{1951, finishFoil, "1951_foil"},
		{1951, finishHolofoil, "1951_holofoil"},
		// A minted card is filed under the negated product id
		{-714954, finishNonfoil, "m-714954"},
		{-714954, finishHolofoil, "m-714954_holofoil"},
	} {
		if got := printingUUID(test.id, test.finish); got != test.want {
			t.Errorf("printingUUID(%d, %q) = %q, want %q", test.id, test.finish, got, test.want)
		}
	}
}

// TestFinishOfSeparatesTreatments is the regression guard for the collapse
// that shipped: three cards are foiled two ways and in neither of the
// standard ones, so both names claimed the treatment slot and merged onto
// one uuid while the standard foil TCGplayer does sell went unreachable by
// any name upstream gives it. Two spellings of two printings must never be
// one printing.
func TestFinishOfSeparatesTreatments(t *testing.T) {
	for _, test := range []struct {
		desc      string
		foilTypes []string
		sold      []string
	}{
		{
			// Whispers in the Well: FreeForm1 is the set's own foil,
			// RainbowPillars the treatment past it.
			desc:      "two treatments and no standard foil",
			foilTypes: []string{"None", "FreeForm1", "RainbowPillars"},
			sold:      []string{tcgNormal, tcgColdFoil, tcgHolofoil},
		},
		{
			desc:      "a standard foil beside one treatment",
			foilTypes: []string{"None", "Silver", "RainbowPillars"},
			sold:      []string{tcgNormal, tcgColdFoil, tcgHolofoil},
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			seen := map[string]string{}
			for _, foilType := range test.foilTypes {
				finish := finishOf(foilType, test.foilTypes, test.sold)
				if finish == "" {
					t.Errorf("%q is sold under no finish", foilType)
					continue
				}
				if !slices.Contains(test.sold, finish) {
					t.Errorf("%q is sold as %q, which the card is not sold in (%v)",
						foilType, finish, test.sold)
				}
				if other, clash := seen[finish]; clash {
					t.Errorf("%q and %q both resolve to %q: two printings under one uuid",
						other, foilType, finish)
				}
				seen[finish] = foilType
			}
			if len(seen) != len(test.sold) {
				t.Errorf("%d foil types reached %d of the %d finishes sold",
					len(test.foilTypes), len(seen), len(test.sold))
			}
		})
	}
}

// TestFinishesSoldNamesTheCatalog pins that the finishes come from the
// catalog's own printing names where it lists them, in its vocabulary,
// deduplicated by the finish they name rather than by spelling.
func TestFinishesSoldNamesTheCatalog(t *testing.T) {
	item := map[string]any{
		"foilTypes": []any{"None", "Silver", "RainbowPillars"},
		"externalLinks": map[string]any{
			"tcgPrintings": []any{tcgColdFoil, tcgHolofoil, tcgNormal},
		},
	}
	got := finishesSold(item)
	want := []string{tcgColdFoil, tcgHolofoil, tcgNormal}
	if !slices.Equal(got, want) {
		t.Errorf("finishesSold = %v, want %v", got, want)
	}

	// The catalog naming none is the only case the foil types decide, and
	// they are spelled into the catalog's vocabulary too.
	bare := map[string]any{"foilTypes": []any{"None", "Silver"}}
	got = finishesSold(bare)
	want = []string{tcgNormal, tcgColdFoil}
	if !slices.Equal(got, want) {
		t.Errorf("finishesSold(no catalog) = %v, want %v", got, want)
	}
}
