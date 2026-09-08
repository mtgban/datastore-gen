package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestFinishSuffixIsDerived pins that nothing about a category's printings
// is written here. TCGplayer names them, adds to them and renames them, and
// every one has to reach an id without a release: the plain printing takes
// the bare id and every other takes its own name.
func TestFinishSuffixIsDerived(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		// The one convention: TCGplayer calls a plain printing "Normal"
		// in every category.
		{"Normal", ""},
		{"normal", ""},
		{"Foil", "_foil"},
		{"Holofoil", "_holofoil"},
		{"Reverse Holofoil", "_reverseholofoil"},
		{"1st Edition", "_1stedition"},
		{"Cold Foil", "_coldfoil"},
		// A printing the category has yet to grow
		{"Prismatic Foil", "_prismaticfoil"},
		{"", ""},
	} {
		if got := finishSuffix(test.in); got != test.want {
			t.Errorf("finishSuffix(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestPlainPrintingComesFromTheCatalog pins that the printing taking the
// bare id is read off the dump, and that a category selling none - Yu-Gi-Oh
// prices by print run and calls no printing plain - says so rather than
// inventing one.
func TestPlainPrintingComesFromTheCatalog(t *testing.T) {
	with := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{Name: "Holofoil"}, {Name: "Normal"},
	}}
	if got := plainPrinting(with); got != "Normal" {
		t.Errorf("plainPrinting = %q, want %q", got, "Normal")
	}
	without := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{Name: "1st Edition"}, {Name: "Unlimited"},
	}}
	if got := plainPrinting(without); got != "" {
		t.Errorf("plainPrinting = %q, want none", got)
	}
}

// TestOrderedFinishesFollowsTheCatalog pins that a product's entries come
// out in the order TCGplayer displays the category's printings, with the
// name settling a tie.
func TestOrderedFinishesFollowsTheCatalog(t *testing.T) {
	rank := map[string]int{"Normal": 1, "Holofoil": 2, "Alpha Foil": 2, "Prismatic Foil": 9}
	got := orderedFinishes([]string{"Prismatic Foil", "Holofoil", "Alpha Foil", "Normal"}, rank)
	want := []string{"Normal", "Alpha Foil", "Holofoil", "Prismatic Foil"}
	if !slices.Equal(got, want) {
		t.Errorf("orderedFinishes = %v, want %v", got, want)
	}
}
