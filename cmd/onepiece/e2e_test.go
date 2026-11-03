package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: OP01 with a Box Topper and a DON!! card,
// the promo shelf with a numberless leader and two printings of P-001, ST-01
// with no abbreviation, and the pre-errata OP01-003 and OP01-007.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "onepiece",
		"-punk-cards", "testdata/punk-cards.json",
		"-punk-packs", "testdata/punk-packs.json",
		"-cardmarket-catalog", "testdata/cardmarket-catalog.json")
}
