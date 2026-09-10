package vocabulary

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// games are every game this repository builds a datastore for.
var games = []string{
	"fleshandblood", "gundam", "lorcana", "onepiece",
	"palworld", "pokemon", "riftbound", "yugioh",
}

// TestPublishedVocabulary reads the datastores under STORE_DIR and holds
// each one to the rules. It is the check to run before publishing: the
// files are built rather than committed, so a run without them says so and
// stops instead of passing on nothing.
func TestPublishedVocabulary(t *testing.T) {
	root := os.Getenv("STORE_DIR")
	if root == "" {
		t.Skip("set STORE_DIR to the directory holding the built datastores")
	}
	var read int
	for _, game := range games {
		t.Run(game, func(t *testing.T) {
			path := filepath.Join(root, game+".json")
			printings, err := ReadDatastore(path)
			switch {
			case errors.Is(err, os.ErrNotExist):
				t.Skipf("%s has not been built", game)
			case errors.Is(err, ErrNotDatastore):
				// Said out loud rather than counted as clean: this file is
				// the upstream document, and the build has not run.
				t.Skipf("%v", err)
			case err != nil:
				t.Fatal(err)
			}
			sets, err := SetNames(path)
			if err != nil {
				t.Fatal(err)
			}
			read++
			found := Check(printings, sets)
			if !found.Any() {
				return
			}
			for _, line := range found.Lines() {
				t.Errorf("%s: %s", game, line)
			}
		})
	}
	if read == 0 {
		// Named, because the likeliest reason is that the directory is not
		// the one meant: go test runs with the package's own directory as
		// its working directory, so a relative STORE_DIR is read from
		// there.
		here, _ := os.Getwd()
		t.Fatalf("STORE_DIR %q held no built datastore; looked for %s, from %s",
			root, filepath.Join(root, "<game>.json"), here)
	}
}
