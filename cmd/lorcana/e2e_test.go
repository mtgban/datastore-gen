package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: The First Chapter, the promo and D23
// shelves, a Panorama foil sold apart, the numberless puzzle insert, Hades
// minted from its product, and a card priced in nothing.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "lorcana",
		"-lorcana", "testdata/lorcana.json",
		"-cardmarket-catalog", "testdata/cardmarket-catalog.json")
}
