package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: GD01, Edition Beta, the promo shelf with two
// rarities on one number, the numberless Resource tokens with no
// abbreviation, a card only gcg-api lists and a hand-carried reprint.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "gundam",
		"-gcg-cards", "testdata/gcg-cards.json")
}
