package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestDecomposeReadsAQualifierOpenOrClosed pins that a parenthesised
// qualifier comes off the name whether or not the catalog closed it, and
// that one which is not the product's rarity is kept.
func TestDecomposeReadsAQualifierOpenOrClosed(t *testing.T) {
	rarities := map[string]string{
		"starlight rare": "Starlight Rare",
		"ultra rare":     "Ultra Rare",
		"secret rare":    "Secret Rare",
	}
	for _, test := range []struct {
		name, number, rarity string
		want                 string
		quals                []string
	}{
		{"Aussa the Earth Charmer (Starlight Rare", "CH02-EN004", "Starlight Rare", "Aussa the Earth Charmer", nil},
		{"Aussa the Earth Charmer (Ultra Rare)", "CH02-EN004", "Ultra Rare", "Aussa the Earth Charmer", nil},
		{"Cyber Dragon (Alternate Art)", "LCGX-EN176", "Secret Rare", "Cyber Dragon", []string{"Alternate Art"}},
	} {
		product := tcgplayer.Product{
			Name: test.name,
			ExtendedData: []tcgplayer.ExtendedField{
				{Name: "Number", Value: test.number},
				{Name: "Rarity", Value: test.rarity},
			},
		}
		got := decompose(product, test.number, rarities)
		if got.baseName != test.want || !slices.Equal(got.quals, test.quals) {
			t.Errorf("decompose(%q) = %q %q, want %q %q", test.name, got.baseName, got.quals, test.want, test.quals)
		}
	}
}
