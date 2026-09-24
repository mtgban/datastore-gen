package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestCanonicalFinish pins how the build classifies a printing or foil type
// name: plain, the standard foil, the Holofoil, or a foil type handed back
// as itself.
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

// TestPrintingUUID pins the uuid a printing prices: the card's own, ending in
// the name TCGplayer prices the printing under, the way every builder ends one.
func TestPrintingUUID(t *testing.T) {
	for _, test := range []struct {
		id     int
		finish string
		want   string
	}{
		{1951, "Normal", "1951"},
		{1951, "Cold Foil", "1951_coldfoil"},
		{1951, "Holofoil", "1951_holofoil"},
		// A minted card is filed under the negated product id
		{-714954, "Normal", "m-714954"},
		{-714954, "Holofoil", "m-714954_holofoil"},
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
			sold:      []string{"Normal", "Cold Foil", "Holofoil"},
		},
		{
			desc:      "a standard foil beside one treatment",
			foilTypes: []string{"None", "Silver", "RainbowPillars"},
			sold:      []string{"Normal", "Cold Foil", "Holofoil"},
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

// catalogVocabulary is category 71's finish vocabulary as the dump carries
// it today, which is what catalogFinishes reads out of it.
var catalogVocabulary = map[string]string{
	finishNonfoil:  "Normal",
	finishFoil:     "Cold Foil",
	finishHolofoil: "Holofoil",
}

// TestCatalogFinishes pins that the vocabulary is read from the dump rather
// than named here: the category's printings are TCGplayer's to rename, and a
// treatment it may add is not an answer to "what is the plain one called".
func TestCatalogFinishes(t *testing.T) {
	dump := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{PrintingID: 132, Name: "Normal"},
		{PrintingID: 133, Name: "Holofoil"},
		{PrintingID: 141, Name: "Cold Foil"},
	}}
	got := catalogFinishes(dump)
	for finish, want := range catalogVocabulary {
		if got[finish] != want {
			t.Errorf("catalogFinishes()[%q] = %q, want %q", finish, got[finish], want)
		}
	}

	// A category renaming a printing is followed, not overruled.
	renamed := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{PrintingID: 132, Name: "Non-Foil"},
		{PrintingID: 141, Name: "Foil"},
	}}
	got = catalogFinishes(renamed)
	if got[finishNonfoil] != "Non-Foil" || got[finishFoil] != "Foil" {
		t.Errorf("a renamed category reads as %v, want the names it now uses", got)
	}

	// A printing the shared vocabulary does not place answers for no finish
	added := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{PrintingID: 132, Name: "Normal"},
		{PrintingID: 200, Name: "Rainbow Foil"},
	}}
	if got = catalogFinishes(added); len(got) != 1 || got[finishNonfoil] != "Normal" {
		t.Errorf("a printing past the vocabulary reads as %v, want only the plain one", got)
	}
}

// TestFinishesSoldNamesTheCatalog pins that the finishes come from the
// catalog's own printing names where it lists them, in its vocabulary,
// deduplicated by the finish they name rather than by spelling.
func TestFinishesSoldNamesTheCatalog(t *testing.T) {
	item := map[string]any{
		"foilTypes": []any{"None", "Silver", "RainbowPillars"},
		"externalLinks": map[string]any{
			"tcgPrintings": []any{"Cold Foil", "Holofoil", "Normal"},
		},
	}
	got := finishesSold(item, catalogVocabulary)
	want := []string{"Cold Foil", "Holofoil", "Normal"}
	if !slices.Equal(got, want) {
		t.Errorf("finishesSold = %v, want %v", got, want)
	}

	// The catalog naming none is the only case the foil types decide, and
	// they are spelled into the catalog's vocabulary too.
	bare := map[string]any{"foilTypes": []any{"None", "Silver"}}
	got = finishesSold(bare, catalogVocabulary)
	want = []string{"Normal", "Cold Foil"}
	if !slices.Equal(got, want) {
		t.Errorf("finishesSold(no catalog) = %v, want %v", got, want)
	}
}

// TestNormalizeNameFoldsAccents pins that the two sources' spellings of one
// name meet: LorcanaJSON writes "Te Kā" and "Félix Madrigal" where the
// catalog writes "Te Ka" and "Felix Madrigal", and a join that kept the
// accent found every one of those cards only because it already carried a
// product id.
func TestNormalizeNameFoldsAccents(t *testing.T) {
	for _, test := range []struct{ a, b string }{
		{"Te Kā - Heartless", "Te Ka - Heartless"},
		{"Félix Madrigal", "Felix Madrigal"},
		{"Ariel - On Human Legs (Foil)", "Ariel - On Human Legs"},
	} {
		if normalizeName(test.a) != normalizeName(test.b) {
			t.Errorf("normalizeName(%q) = %q, normalizeName(%q) = %q; want the same key",
				test.a, normalizeName(test.a), test.b, normalizeName(test.b))
		}
	}
}
