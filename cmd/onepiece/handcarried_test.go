package main

import (
	"encoding/json"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/validate"
)

// TestCarryHandPrintedWithoutItsSet pins a catalog with no OP-RP group: the
// Revision Pack reissue of OP01-005 is set aside, its pre-errata still
// carried, and the build re-reads clean instead of refusing an unknown set.
func TestCarryHandPrintedWithoutItsSet(t *testing.T) {
	var rows []handCarried
	for _, row := range handCarriedPrintings {
		if row.number == "OP01-005" {
			rows = append(rows, row)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("OP01-005 has %d hand-carried rows, want its pre-errata and its OP-RP reissue", len(rows))
	}

	carry := func(sets map[string]*emit.Set) ([]*emit.Card, map[string]bool) {
		uta := &emit.Card{
			ID: "op01-005_100", Name: "Uta", SetCode: "OP01", Number: "OP01-005",
			Rarity: "R", Finish: "foil", Image: "https://tcgplayer-cdn.tcgplayer.com/product/100_in_1000x1000.jpg",
			ExternalLinks: emit.Links{TCGPlayerID: 100},
		}
		cards := []*emit.Card{uta}
		printed := map[string][]*emit.Card{"OP01-005|": cards}
		carried := map[string]bool{"Uta|OP01-005|OP01|": true}
		return carryHandPrinted(rows, cards, sets, printed, carried, map[string]bool{},
			map[string]string{}, map[string]bool{}, map[*emit.Card]stated{}), carried
	}

	sets := map[string]*emit.Set{"OP01": {Name: "Romance Dawn", ReleaseDate: "2022-12-02"}}
	cards, carried := carry(sets)
	if len(cards) != 2 {
		t.Fatalf("carried %d cards, want the parent and its pre-errata", len(cards))
	}
	if got := cards[1]; got.SetCode != "OP01" || got.Variant != "Pre-Errata" {
		t.Errorf("carried %s %q, want the OP01 Pre-Errata", got.SetCode, got.Variant)
	}
	// A row set aside claims nothing, so the day a product carries it no
	// stand-down is owed.
	if carried["Uta|OP01-005|OP-RP|"] {
		t.Error("the set-aside reissue claimed its identity")
	}

	doc := &emit.Datastore{Game: "onepiece", Sets: sets, Cards: cards}
	data, err := json.Marshal(emit.Envelope(emit.Today(), doc))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validate.Datastore(data, map[int][]string{100: {"foil"}}, validationRules()); err != nil {
		t.Errorf("the build without OP-RP was refused: %v", err)
	}

	// The day the catalog holds the group, the reissue is carried in it.
	sets["OP-RP"] = &emit.Set{Name: "Revision Pack Cards"}
	if cards, _ := carry(sets); len(cards) != 3 || cards[2].SetCode != "OP-RP" {
		t.Errorf("with OP-RP held, carried %d cards, want the reissue in OP-RP too", len(cards))
	}
}
