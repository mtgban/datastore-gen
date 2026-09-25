package main

import (
	"strings"
	"testing"
)

// catalogEntry is an entry as the catalog pass builds it, before anything
// links a Cardmarket product to it.
func catalogEntry(id string, product int) map[string]any {
	return map[string]any{"id": id, "externalLinks": map[string]any{"tcgPlayerId": product}}
}

// TestLinkCardmarketProducts pins the O-Nami pair productCardmarketIDs exists
// for: each TCGplayer product's one entry gets its own Cardmarket product.
func TestLinkCardmarketProducts(t *testing.T) {
	dash := catalogEntry("op05-062_712033_foil", 712033)
	box := catalogEntry("op05-062_623070_foil", 623070)
	other := catalogEntry("op05-062_600000", 600000)
	linked, _ := linkCardmarketProducts([]any{dash, box, other})
	if linked != 2 {
		t.Errorf("linked %d entries, want the 2 O-Nami ones", linked)
	}
	for _, tc := range []struct {
		entry map[string]any
		want  any
	}{{dash, 874419}, {box, 821356}, {other, nil}} {
		if got := tc.entry["externalLinks"].(map[string]any)["cardmarketId"]; got != tc.want {
			t.Errorf("%s cardmarketId = %v, want %v", tc.entry["id"], got, tc.want)
		}
	}
}

// TestLinkCardmarketProductsStandsDown pins the three ways a row reports
// instead of applying.
func TestLinkCardmarketProductsStandsDown(t *testing.T) {
	has := func(reports []string, want string) bool {
		for _, r := range reports {
			if strings.Contains(r, want) {
				return true
			}
		}
		return false
	}

	// No entry for the product at all: every row reports.
	if _, reports := linkCardmarketProducts(nil); len(reports) != len(productCardmarketIDs) || !has(reports, "712033 carries no entry") {
		t.Errorf("with no entries, reported %v", reports)
	}

	// The product in two finishes: which one Cardmarket sells is unsaid.
	plain, foil := catalogEntry("op05-062_712033", 712033), catalogEntry("op05-062_712033_foil", 712033)
	if _, reports := linkCardmarketProducts([]any{plain, foil}); !has(reports, "712033 is carried in 2 finishes") {
		t.Errorf("with two finishes, reported %v", reports)
	}
	if plain["externalLinks"].(map[string]any)["cardmarketId"] != nil || foil["externalLinks"].(map[string]any)["cardmarketId"] != nil {
		t.Error("a product in two finishes was given a Cardmarket id")
	}

	// Another entry already carries the id, as a hand-carried one would.
	carried := map[string]any{"id": "op05-062_ct1_foil", "externalLinks": map[string]any{"cardmarketId": 874419}}
	dash := catalogEntry("op05-062_712033_foil", 712033)
	if _, reports := linkCardmarketProducts([]any{carried, dash}); !has(reports, "712033 stands down: op05-062_ct1_foil") {
		t.Errorf("with the id carried elsewhere, reported %v", reports)
	}
	if dash["externalLinks"].(map[string]any)["cardmarketId"] != nil {
		t.Error("the id was written a second time")
	}
}
