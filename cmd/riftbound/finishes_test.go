package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// The uuids this build publishes are spelled from a finish name, and
// go-mtgban's mtgmatcher spells the same names for the datastores that carry
// none. Neither repository imports the other - this one is standalone by
// design - so the agreement is held by this table rather than by the
// compiler.

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
		// The spelling the datastores built before this one carry
		{"nonfoil", "nonfoil"},
		{"foil", "foil"},
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

// TestFinishesByProduct pins what the catalog decides: a product is sold in
// the printings it has skus under, named as TCGplayer names them and ordered
// by the catalog's own printing ids so unchanged data keeps producing
// byte-identical output.
func TestFinishesByProduct(t *testing.T) {
	// Normal is 173 and Foil is 176 in category 89, so the id order is the
	// one that puts the plain printing first.
	dump := &tcgplayer.CatalogDump{
		Printings: []tcgplayer.Printing{
			{PrintingID: 176, Name: "Foil"},
			{PrintingID: 173, Name: "Normal"},
		},
		Products: []tcgplayer.Product{
			{ProductID: 1, Skus: []tcgplayer.SKU{{PrintingID: 176}, {PrintingID: 173}}},
			{ProductID: 2, Skus: []tcgplayer.SKU{{PrintingID: 176}}},
			// The same printing twice is one printing
			{ProductID: 3, Skus: []tcgplayer.SKU{{PrintingID: 173}, {PrintingID: 173}}},
			// A printing the dump names nothing for counts for no finish
			{ProductID: 4, Skus: []tcgplayer.SKU{{PrintingID: 999}}},
		},
	}

	got := finishesByProduct(dump)
	for _, test := range []struct {
		product int
		want    []string
	}{
		{1, []string{"Normal", "Foil"}},
		{2, []string{"Foil"}},
		{3, []string{"Normal"}},
	} {
		if !slices.Equal(got[test.product], test.want) {
			t.Errorf("product %d is sold in %v, want %v", test.product, got[test.product], test.want)
		}
	}
	if _, found := got[4]; found {
		t.Errorf("product 4 has skus under no printing this build knows, want no finish at all, got %v", got[4])
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
			{PrintingID: 173, Name: "Normal"},
			{PrintingID: 400, Name: "Holofoil"},
		},
		Products: []tcgplayer.Product{
			{ProductID: 1, Skus: []tcgplayer.SKU{{PrintingID: 400}, {PrintingID: 173}}},
		},
	}
	got := finishesByProduct(dump)[1]
	want := []string{"Normal", "Holofoil"}
	if !slices.Equal(got, want) {
		t.Fatalf("a printing added to the category is sold as %v, want %v", got, want)
	}
	if canonicalFinish("Holofoil") != "holofoil" {
		t.Error("the added printing reaches no finish of its own")
	}
}
