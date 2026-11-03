package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: WTR in four printings, ROS with a Marvel,
// a numberless card and a token only the dataset lists, an Armory Deck with
// no abbreviation, and a promo from a set the catalog has no group for.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "fleshandblood",
		"-fab-cards", "testdata/fab-cards.json",
		"-fab-sets", "testdata/fab-sets.json")
}
