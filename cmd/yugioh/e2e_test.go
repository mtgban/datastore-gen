package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: LOB in both editions with Dark Magician's
// European print minted, the Shonen Jump prizes with no abbreviation, and a
// numberless Mattel promo.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "yugioh",
		"-ygoprodeck-sets", "testdata/ygoprodeck-sets.json",
		"-ygoprodeck-cards", "testdata/ygoprodeck-cards.json")
}
