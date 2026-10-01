package main

import (
	"slices"
	"testing"
)

// lorcanaCard is one upstream card as the build decodes it: an id and the
// product it names, 0 for none.
func lorcanaCard(id int, name string, product int) map[string]any {
	card := map[string]any{"id": float64(id), "fullName": name, "externalLinks": map[string]any{}}
	if product != 0 {
		card["externalLinks"].(map[string]any)["tcgPlayerId"] = float64(product)
	}
	return card
}

// TestOneCardPerID pins what a repeated upstream id becomes: an exact repeat
// is the same card twice and goes, a different card keeps its place on the
// id its own product spells, and one naming no product goes.
func TestOneCardPerID(t *testing.T) {
	a := lorcanaCard(1624, "Tick-Tock - Relentless Crocodile", 619517)
	items := []any{
		a,
		lorcanaCard(1624, "Tick-Tock - Relentless Crocodile", 619517), // exact repeat
		lorcanaCard(1624, "Kakamora - Band of Pirates", 619518),       // a different card
		lorcanaCard(1624, "Unpriced Stranger", 0),                     // a different card, no product
		lorcanaCard(1625, "Kakamora - Band of Pirates", 619519),       // its own id, untouched
	}
	kept, repeats := oneCardPerID(items)

	var got []int
	for _, item := range kept {
		id, _ := cardID(item.(map[string]any)["id"])
		got = append(got, id)
	}
	if want := []int{1624, -619518, 1625}; !slices.Equal(got, want) {
		t.Errorf("ids kept = %v, want %v", got, want)
	}
	if len(repeats) != 3 {
		t.Errorf("reported %d repeats %v, want 3", len(repeats), repeats)
	}
	if kept[0].(map[string]any)["fullName"] != a["fullName"] {
		t.Error("the first card under an id must keep it")
	}
}

// TestOneCardPerPrintingKeepsTheLowerID pins that two cards under one
// fullIdentifier are one printing, as Moana and Vaiana are, and that the
// card with the lower id is kept whichever comes first.
func TestOneCardPerPrintingKeepsTheLowerID(t *testing.T) {
	vaiana := map[string]any{"id": 1663.0, "fullName": "Vaiana - Adventurer of Land and Sea", "fullIdentifier": "26/P2 • EN • 7"}
	moana := map[string]any{"id": 1433.0, "fullName": "Moana - Adventurer of Land and Sea", "fullIdentifier": "26/P2 • EN • 7"}
	other := map[string]any{"id": 1434.0, "fullName": "Maui", "fullIdentifier": "27/P2 • EN • 7"}
	kept, skipped := oneCardPerPrinting([]any{vaiana, moana, other})
	if len(kept) != 2 || kept[0].(map[string]any)["id"] != 1433.0 || len(skipped) != 1 {
		t.Errorf("kept %v, skipped %q; want Moana and Maui kept, Vaiana skipped", kept, skipped)
	}
}
