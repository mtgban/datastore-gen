package main

import (
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestDecomposeTakesTheNumberOffTheName pins that a collector number the
// catalog hangs on a name comes off it however the dash is spaced, and that
// a tail which does not restate the Number field stays.
func TestDecomposeTakesTheNumberOffTheName(t *testing.T) {
	for _, test := range []struct {
		name, num, want string
	}{
		{"Exploud - 3/106", "3", "Exploud"},
		{"Gyarados 21/98", "21", "Gyarados"},
		{"Ninetales -199/197", "199", "Ninetales"},
		{"Mimikyu -160/091", "160", "Mimikyu"},
		{"Bouffalant -119/142", "119", "Bouffalant"},
		{"Porygon -Z", "1", "Porygon -Z"},
		{"Ninetales -199/197", "200", "Ninetales -199/197"},
	} {
		got, _ := decompose(tcgplayer.Product{Name: test.name}, test.num, "")
		if got.baseName != test.want {
			t.Errorf("decompose(%q, number %q) = %q, want %q", test.name, test.num, got.baseName, test.want)
		}
	}
}
