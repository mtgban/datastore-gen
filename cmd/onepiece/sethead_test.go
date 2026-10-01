package main

import "testing"

// TestCutHead pins reading a set's name off a label by letters and digits:
// the labels kept the wording the starter decks had before TCGplayer put a
// code in front of every one of them on 2026-09-29.
func TestCutHead(t *testing.T) {
	for _, test := range []struct {
		label, head, rest string
		opens             bool
	}{
		{"starter deck 11: uta deck battle", "starter deck 11 uta", "deck battle", true},
		{"starter deck 11: uta deck battle", "starter deck 11: uta", "deck battle", true},
		{"ultra deck: the three captains", "ultra deck the three captains", "", true},
		{"starter deck 11: uta", "starter deck 1 straw hat crew", "", false},
		{"starter deck 11 utah deck battle", "starter deck 11 uta", "", false},
		{"deck battle", "starter deck 11 uta", "", false},
	} {
		rest, opens := cutHead(test.label, test.head)
		if rest != test.rest || opens != test.opens {
			t.Errorf("cutHead(%q, %q) = %q, %v, want %q, %v", test.label, test.head, rest, opens, test.rest, test.opens)
		}
	}
}
