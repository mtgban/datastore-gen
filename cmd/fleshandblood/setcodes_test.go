package main

import (
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestSetCodesNeverShareARepairedCode pins that a group whose suffixed code
// another group already claimed is carried on a code of its own: group 3
// finds both X and X-3 taken, so it is given X-3-2.
func TestSetCodesNeverShareARepairedCode(t *testing.T) {
	codes := setCodes([]tcgplayer.Group{
		{GroupID: 1, Abbreviation: "X"},
		{GroupID: 2, Abbreviation: "X-3"},
		{GroupID: 3, Abbreviation: "X"},
	})
	for group, want := range map[int]string{1: "X", 2: "X-3", 3: "X-3-2"} {
		if codes[group] != want {
			t.Errorf("group %d: %q, want %q", group, codes[group], want)
		}
	}
}
