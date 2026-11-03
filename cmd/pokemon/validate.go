package main

import (
	"fmt"
	"strings"

	"github.com/mtgban/datastore-gen/internal/validate"
)

// properties orders the values of the fields a consumer ranks cards by.
// Rarities run rarest first across the eras: the current symbols' ladder,
// each older or one-set rarity beside the tier it pulls like, promos first
// and the code cards and unconfirmed rows last. Types are in the game's
// order, Colorless last.
var properties = map[string][]string{
	"rarity": {
		"Promo", "RGB Rare", "Mega Hyper Rare", "Black White Rare",
		"Hyper Rare", "Futuristic Rare", "Secret Rare", "Rainbow Rare",
		"Special Illustration Rare", "Mega Attack Rare", "Shiny Ultra Rare",
		"Illustration Rare", "Shiny Rare", "Ultra Rare", "ACE SPEC Rare",
		"Amazing Rare", "Radiant Rare", "Shiny Holo Rare", "Prism Rare",
		"Rare Ace", "Rare BREAK", "Double Rare", "Classic Collection",
		"Pikachu Rare", "Holo Rare", "Rare", "Uncommon", "Common", "Code Card",
		"Unconfirmed", "None",
	},
	"type": {
		"Grass", "Fire", "Water", "Lightning", "Psychic", "Fighting", "Darkness",
		"Metal", "Fairy", "Dragon", "Colorless",
	},
}

// validationRules are what this game adds to the shared re-read of the
// output. The total is in the identity, which is what tells 8/102 from
// 8/130, and a minted entry is keyed by its set and number, so a minted
// card's own finishes pass together the way a product's do. A number holds
// no total.
func validationRules() validate.Rules {
	return validate.Rules{
		Game:     "pokemon",
		Fields:   map[string]string{"type": "types"},
		Identity: []string{"name", "number", "total", "setCode", "variant", "rarity"},
		Minted: func(card validate.Card) string {
			return "minted:" + card.SetCode + "|" + card.Number
		},
		Check: func(card validate.Card) error {
			if card.SetCode == "" {
				return fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
			}
			if strings.Contains(card.Number, "/") {
				return fmt.Errorf("card %q (%s) carries the set total inside its number: %q", card.Name, card.ID, card.Number)
			}
			return nil
		},
	}
}
