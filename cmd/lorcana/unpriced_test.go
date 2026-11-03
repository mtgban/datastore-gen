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
		{ProductID: 485360, Name: "Scar - Mastermind", ProductType: tcgSingles[0]},
		{ProductID: 485361, Name: "Aladdin - Prince Ali", ProductType: tcgSingles[0]},
		{ProductID: 482406, Name: "Disney Lorcana: The First Chapter Booster Pack", ProductType: "Sealed Products"},
	}
	printings := map[int][]string{485360: {"Cold Foil", "Normal"}, 482406: {"Normal"}}
	priced, unpriced, names := pricedSingles(products, printings)
	if !priced[485360] || priced[485361] || priced[482406] || len(priced) != 1 {
		t.Errorf("priced = %v, want only 485360", priced)
	}
	if !unpriced[485361] || len(unpriced) != 1 {
		t.Errorf("unpriced = %v, want only 485361", unpriced)
	}
	if want := []string{`"Aladdin - Prince Ali" (485361)`}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
}

// TestUnlinkUnpriced pins what upstream's card for an unpriced product
// becomes: a card naming no product, whose finishes are upstream's, and a
// card naming a priced product is left alone.
func TestUnlinkUnpriced(t *testing.T) {
	unpriced := map[int]bool{485361: true}
	raw := lorcanaCard(69, "Aladdin - Prince Ali", 485361)
	raw["setCode"] = "1"
	raw["foilTypes"] = []any{"None", "Silver"}
	raw["externalLinks"].(map[string]any)["tcgPlayerUrl"] = "https://www.tcgplayer.com/product/485361"
	aladdin, ok := decodeCard(raw)
	if !ok {
		t.Fatal("decodeCard refused the card")
	}
	if product := unlinkUnpriced(&aladdin, unpriced); product != 485361 {
		t.Errorf("unlinkUnpriced = %d, want 485361", product)
	}
	if aladdin.tcgID != 0 || len(aladdin.links) != 0 {
		t.Errorf("unlinked card keeps tcgID %d, links %v", aladdin.tcgID, aladdin.links)
	}
	finishNames := map[string]string{finishNonfoil: "Normal", finishFoil: "Cold Foil"}
	if got, want := finishesSold(raw, finishNames), []string{"Normal", "Cold Foil"}; !slices.Equal(got, want) {
		t.Errorf("finishesSold = %v, want upstream's %v", got, want)
	}

	scar := lorcanaCard(158, "Scar - Mastermind", 485360)
	scar["setCode"] = "1"
	priced, _ := decodeCard(scar)
	if product := unlinkUnpriced(&priced, unpriced); product != 0 || priced.tcgID != 485360 {
		t.Errorf("a priced card was unlinked: returned %d, tcgID %d", product, priced.tcgID)
	}
}
