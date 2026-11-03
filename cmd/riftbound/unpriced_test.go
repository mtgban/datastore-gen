package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestPricedSingles pins the coverage split: a single priced in nothing yet
// is set aside rather than owed an entry, and a sealed product is neither.
func TestPricedSingles(t *testing.T) {
	products := []tcgplayer.Product{
		{ProductID: 684202, Name: "Abandon", ProductType: tcgSingles[0]},
		{ProductID: 684123, Name: "Crowd Favorite", ProductType: tcgSingles[0]},
		{ProductID: 678149, Name: "Unleashed - Booster Pack", ProductType: "Sealed Products"},
	}
	finishes := map[int][]string{684202: {"Normal", "Foil"}, 678149: {"Normal"}}
	priced, unpriced := pricedSingles(products, finishes)
	if !priced[684202] || len(priced) != 1 {
		t.Errorf("priced = %v, want only 684202", priced)
	}
	if want := []string{`"Crowd Favorite" (684123)`}; !slices.Equal(unpriced, want) {
		t.Errorf("unpriced = %q, want %q", unpriced, want)
	}
}
