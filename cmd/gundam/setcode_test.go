package main

import "testing"

// TestSetCodeOfFolds pins that a set code leaves this build folded up.
// A set code is a case-insensitive token to every reader of one: go-mtgban's
// GetSet, GetUUIDsInSet and GetSealedUUIDsInSet each fold the caller's
// spelling before the lookup, so a code that is not already folded is listed
// everywhere and found nowhere.
//
// TCGplayer abbreviates Edition Beta "GD01_b", the one mixed-case
// abbreviation in category 86, and the "GD01-b" this used to mint took its
// set, its 83 cards and the two sealed products sold with them out of reach:
// the website's "s:GD01-b" came back empty and every link into the set was
// dead.
func TestSetCodeOfFolds(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		// The abbreviation that broke, and the rest of the shelf it is on
		{"GD01_b", "GD01-B"},
		{"GD01", "GD01"},
		{"ST01", "ST01"},
		// Whatever case the catalog writes, and whatever it separates with
		{"gd01_b", "GD01-B"},
		{"GCG PR", "GCG-PR"},
		{"-EVX01-", "EVX01"},
		{"", ""},
	} {
		got := setCodeOf(test.in)
		if got != test.want {
			t.Errorf("setCodeOf(%q) = %q, want %q", test.in, got, test.want)
		}
		if got != "" && !codeShape.MatchString(got) {
			t.Errorf("setCodeOf(%q) = %q, which the build's own check refuses", test.in, got)
		}
	}
}
