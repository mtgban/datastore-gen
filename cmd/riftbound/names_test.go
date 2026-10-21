package main

import (
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

func product(name, number string) tcgplayer.Product {
	return tcgplayer.Product{Name: name, ExtendedData: []tcgplayer.ExtendedField{{Name: "Number", Value: number}}}
}

// TestSameCard pins which products print one card: the alternate art and
// the signature of a number are its card, the next number is another.
func TestSameCard(t *testing.T) {
	leader := sameCard(product("Viktor, Leader", "246/298"))
	if got := sameCard(product("Viktor, Leader (Alternate Art)", "246a/298")); got != leader {
		t.Errorf("246a keys %q, want 246's %q", got, leader)
	}
	exalted := sameCard(product("Aphelios, Exalted (Heartsteel Overnumbered)", "179/167"))
	if got := sameCard(product("Aphelios, Exalted (Heartsteel Signature)", "179*/167")); got != exalted {
		t.Errorf("179* keys %q, want 179's %q", got, exalted)
	}
	if got := sameCard(product("Viktor, Leader", "247/298")); got == leader {
		t.Errorf("247 keys %q, the same as 246", got)
	}
}

// TestAdoptedCardTakesTheGalleryName pins a printing only the catalog sells:
// it goes by the gallery's name for the card and keeps the labels the
// catalog's qualifiers give it.
func TestAdoptedCardTakesTheGalleryName(t *testing.T) {
	alternate := product("Viktor, Leader (Alternate Art)", "246a/298")
	card := adoptedCard(tcgplayer.Group{Abbreviation: "OGN"}, alternate, "246a/298", nil, "Viktor")
	if card["name"] != "Viktor" || card["variant"] != "Alternate Art" {
		t.Errorf("adopted as %q, variant %q; want Viktor, Alternate Art", card["name"], card["variant"])
	}
	card = adoptedCard(tcgplayer.Group{Abbreviation: "OGN"}, alternate, "246a/298", nil, "")
	if card["name"] != "Viktor, Leader" {
		t.Errorf("with no gallery name adopted as %q, want the catalog's", card["name"])
	}
}
