package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: BP01 with the Soul the catalog leaves
// unnumbered, the promo shelf, the sleeve set with no abbreviation, and a
// booster card only upstream lists.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "palworld",
		"-palworld-cards", "testdata/palworld-cards.json")
}
