package main

import (
	"strings"
	"testing"
)

// TestTypesOf pins where a Pokemon's types come from: tcgdex where it
// types the card cleanly, the catalog's Card Type where it names types and
// nothing else, and nothing for a Trainer or an Energy card.
func TestTypesOf(t *testing.T) {
	pokemon := func(types ...string) *tcgdexCard { return &tcgdexCard{Category: "Pokemon", Types: types} }
	for _, test := range []struct {
		desc     string
		dex      *tcgdexCard
		cardType string
		want     string
	}{
		{"tcgdex answers first", pokemon("Fire"), "Water", "Fire"},
		{"in both of a dual type", pokemon("Grass", "Darkness"), "", "Grass;Darkness"},
		{"a type tcgdex repeats is a typo, so the catalog answers", pokemon("Darkness", "Darkness"), "Darkness Fire", "Darkness;Fire"},
		{"the catalog answers where tcgdex does not know the card", nil, "Lightning", "Lightning"},
		{"through its other spellings", nil, "Normal", "Colorless"},
		{"a dual type written with a slash", nil, "Fighting/Darkness", "Fighting;Darkness"},
		{"a Card Type naming anything else is no type", nil, "Basic Fire Energy", ""},
		{"nor is a Trainer's", nil, "Trainer - Item", ""},
		{"and tcgdex's Trainer has none whatever the catalog says", &tcgdexCard{Category: "Trainer"}, "Fire", ""},
		{"as its typed Energy has none", &tcgdexCard{Category: "Energy", Types: []string{"Fighting"}}, "", ""},
	} {
		got := strings.Join((&printedTypes{}).of(test.dex, test.cardType), ";")
		if got != test.want {
			t.Errorf("%s: %q, want %q", test.desc, got, test.want)
		}
	}
}
