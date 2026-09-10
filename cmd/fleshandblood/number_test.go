package main

import "testing"

// TestSpelledNumberTakesTheNameOverThePadding pins the one case the name
// outranks the Number field: the same number, padded with a zero the card
// does not print. A tail that disagrees in any other way stays the field's.
func TestSpelledNumberTakesTheNameOverThePadding(t *testing.T) {
	for _, test := range []struct{ name, num, want string }{
		{"Dash I/O - HER156", "HER0156", "HER156"},
		{"Boltyn - HER160", "HER0160", "HER160"},
		{"Brutus, Summa Rudis - JDG077", "JDG0077", "JDG077"},
		// The field and the name agree: nothing to take.
		{"Snatch - WTR100", "WTR100", "WTR100"},
		// A different number is a different number, whichever is right.
		{"Dig In (Yellow) - FAB385", "FAB384", "FAB384"},
		// No tail, no field: as they were.
		{"Snatch", "WTR100", "WTR100"},
		{"Snatch - WTR100", "", ""},
	} {
		if got := spelledNumber(test.name, test.num); got != test.want {
			t.Errorf("spelledNumber(%q, %q) = %q, want %q", test.name, test.num, got, test.want)
		}
	}
}

// TestFoldPaddingIsBlindToZeros pins that the fold compares and never names:
// it equates the paddings of one number and keeps a zero that is the digit.
func TestFoldPaddingIsBlindToZeros(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"HER0156", "HER156"},
		{"HER156", "HER156"},
		{"WTR040 // WTR113", "WTR40 // WTR113"},
		{"FAB000", "FAB0"},
	} {
		if got := foldPadding(test.in); got != test.want {
			t.Errorf("foldPadding(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
