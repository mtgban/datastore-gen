package main

import (
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestNumberlessProductPublishesNoNumber pins SPECIFICATIONS §2.2 and §2.4
// on a product the catalog files with no number: it publishes none, and its
// id is the adopted printing's own, spelled from the product.
func TestNumberlessProductPublishesNoNumber(t *testing.T) {
	buff := product("Buff // Buff (Fist Bump)", "")
	buff.ProductID = 663004
	card := adoptedCard(tcgplayer.Group{Abbreviation: "PR"}, "PR", buff, numberFor(buff), []string{"Foil"}, "")
	stampNumber(card)
	if card["id"] != "pr-663004" {
		t.Errorf("id %q, want pr-663004", card["id"])
	}
	for _, field := range []string{"publicCode", "number"} {
		if value, found := card[field]; found {
			t.Errorf("published %s %q, want none", field, value)
		}
	}

	blank := product("Buff", " ")
	blank.ProductID = 653117
	card = adoptedCard(tcgplayer.Group{Abbreviation: "OGN"}, "OGN", blank, numberFor(blank), nil, "")
	stampNumber(card)
	if value, found := card["publicCode"]; found {
		t.Errorf("a blank number published code %q, want none", value)
	}

	numbered := product("Buff", "T01")
	numbered.ProductID = 653117
	card = adoptedCard(tcgplayer.Group{Abbreviation: "OGN"}, "OGN", numbered, numberFor(numbered), nil, "")
	stampNumber(card)
	if card["id"] != "ogn-653117" || card["publicCode"] != "OGN-T01" || card["number"] != "T01" {
		t.Errorf("numbered as %q, code %q, number %q; want ogn-653117, OGN-T01, T01", card["id"], card["publicCode"], card["number"])
	}
}
