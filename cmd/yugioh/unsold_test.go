package main

import (
	"slices"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// TestDropUnsoldKeepsTheEuropeanPrints pins that an entry naming no product
// is kept only when it is a European first print carrying its passcode.
func TestDropUnsoldKeepsTheEuropeanPrints(t *testing.T) {
	entry := func(id, number string, product, passcode int) *emit.Card {
		return &emit.Card{ID: id, Number: number, Name: "Dark Magician",
			ExternalLinks: emit.Links{TCGPlayerID: product, KonamiID: passcode}}
	}
	kept, dropped := dropUnsold([]*emit.Card{
		entry("lob-en005", "LOB-EN005", 100, 46986414),
		entry("lob-e005", "LOB-E005", 0, 46986414),
		entry("lob-e006", "LOB-E006", 0, 0),
		entry("lob-005", "LOB-005", 0, 46986414),
	})
	var ids []string
	for _, c := range kept {
		ids = append(ids, c.ID)
	}
	if !slices.Equal(ids, []string{"lob-en005", "lob-e005"}) || len(dropped) != 2 {
		t.Errorf("kept %v, dropped %q; want the passcodeless and the non-European dropped", ids, dropped)
	}
}
