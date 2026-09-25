package main

import (
	"slices"
	"testing"
)

// TestKeptQuals pins which labels a TCGplayer qualifier carries: the
// promotion, a prize's placement beside it, and nothing for a number, a
// finish or a puzzle piece's place.
func TestKeptQuals(t *testing.T) {
	for _, test := range []struct {
		qual string
		want []string
	}{
		{"Puzzle Promo", []string{"Puzzle"}},
		{"Magical Places Promo", []string{"Magical Places"}},
		{"Store Championship", []string{"Store Championship"}},
		{"Store Championship Participant", []string{"Store Championship", "Participant"}},
		{"Store Champion Participant", []string{"Store Championship", "Participant"}},
		{"Disney Lorcana Challenge Top 128", []string{"Challenge", "Top 128"}},
		{"Disney Lorcana Challenge", []string{"Challenge"}},
		{"Alternate Art", []string{"Alternate Art"}},
		{"1/C1", nil},
		{"6/CC1", nil},
		{"5", nil},
		{"Foil", nil},
		{"Top Left", nil},
		{"Set of 9", nil},
	} {
		if got := keptQuals(test.qual); !slices.Equal(got, test.want) {
			t.Errorf("keptQuals(%q) = %q, want %q", test.qual, got, test.want)
		}
	}
}
