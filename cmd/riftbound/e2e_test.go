package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/buildtest"
)

// TestBuild builds the fixture: Unleashed with an Alternate Art the gallery
// lacks and a gallery card nothing prices, and the promo shelf and Secret
// Garden, with no abbreviation, carrying a numberless Buff and a token.
func TestBuild(t *testing.T) {
	buildtest.Run(t, "riftbound",
		"-gallery", "testdata/gallery.json")
}
