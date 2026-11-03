package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: Jungle, 151 with a numberless code card, the
// Professor Program shelf, a group with no abbreviation, a card only tcgdex
// lists and a stamp only Cardmarket lists.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "pokemon",
		"-tcgdex-sets", "testdata/tcgdex-sets.json",
		"-tcgdex-cards", "testdata/tcgdex-cards.json",
		"-pokemontcg-sets", "testdata/pokemontcg-sets.json",
		"-cardmarket-catalog", "testdata/cardmarket-catalog.json")
}
