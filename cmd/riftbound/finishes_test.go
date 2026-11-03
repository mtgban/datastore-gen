package main

import (
	"slices"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/go-tcgplayer"
)

// The uuids this build publishes are spelled from a finish name, and a
// published uuid that changes spelling resolves to nothing, so the spelling
// is held by this table.

// TestCanonicalFinish pins the crossing between the catalog's printing names
// and the matcher's finish names. The vocabulary is open on purpose: the
// printings under a TCGplayer category are the vendor's to add, so a name
// this build has not been taught is normalized and handed back rather than
// refused.
func TestCanonicalFinish(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		// TCGplayer's whole vocabulary for the category today
		{"Normal", "nonfoil"},
		{"Foil", "foil"},
		// However a source writes them
		{"NORMAL", "nonfoil"},
		{"Non-Foil", "nonfoil"},
		// A printing the category does not have yet
		{"Holofoil", "holofoil"},
		{"Cold Foil", "coldfoil"},
		{"", ""},
	} {
		if got := canonicalFinish(test.in); got != test.want {
			t.Errorf("canonicalFinish(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestUnknownPrintingIsCarried is the reason the vocabulary is open. A
// printing TCGplayer adds to the category has to reach a uuid as data: this
// build carries the name, and the loader places it by normalizing. Dropping
// it instead would leave a real priced printing with no finish at all, and
// the loader reads a product naming none as sold in both.
func TestUnknownPrintingIsCarried(t *testing.T) {
	dump := &tcgplayer.CatalogDump{
		Printings: []tcgplayer.Printing{
			{PrintingID: 173, Name: "Normal", DisplayOrder: 1},
			{PrintingID: 400, Name: "Holofoil", DisplayOrder: 2},
		},
		Products: []tcgplayer.Product{
			{ProductID: 1, Skus: []tcgplayer.SKU{{PrintingID: 400}, {PrintingID: 173}}},
		},
	}
	got := emit.PrintingNames(dump)[1]
	want := []string{"Normal", "Holofoil"}
	if !slices.Equal(got, want) {
		t.Fatalf("a printing added to the category is sold as %v, want %v", got, want)
	}
	if canonicalFinish("Holofoil") != "holofoil" {
		t.Error("the added printing reaches no finish of its own")
	}
}
