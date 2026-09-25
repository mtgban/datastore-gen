package main

import (
	"encoding/json"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

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

// catalogProduct is a card product as the dump carries it: a name and the
// catalog's own Number and Pitch Value.
func catalogProduct(t *testing.T, name, number, pitch string) tcgplayer.Product {
	t.Helper()
	var p tcgplayer.Product
	raw := `{"name": ` + jsonString(name) + `, "extendedData": [{"name": "Number", "value": ` + jsonString(number) +
		`}, {"name": "Pitch Value", "value": ` + jsonString(pitch) + `}]}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestOwnNumberTakesTheDatasetsPitch pins the two products the catalog files
// at the Red's number and the cases the catalog's number stands.
func TestOwnNumberTakesTheDatasetsPitch(t *testing.T) {
	idx := newPitchIndex([]fabRow{
		{ID: "TNP019", Name: "Staunch Response", Pitch: "1"},
		{ID: "TNP020", Name: "Staunch Response", Pitch: "2"},
		{ID: "TNP021", Name: "Staunch Response", Pitch: "3"},
		{ID: "FAB384", Name: "Dig In", Pitch: "1"},
		{ID: "FAB385", Name: "Dig In", Pitch: "2"},
		// A pitch printed twice in one set names neither number.
		{ID: "WTR100", Name: "Snatch", Pitch: "1"},
		{ID: "WTR101", Name: "Snatch", Pitch: "2"},
		{ID: "WTR199", Name: "Snatch", Pitch: "2"},
	})
	for _, test := range []struct {
		name, number, pitch, want string
		moved                     bool
	}{
		{"Staunch Response (Yellow) (Marvel) - TNP020", "TNP019", "2", "TNP020", true},
		{"Staunch Response (Blue) (Marvel) - TNP020", "TNP019", "3", "TNP021", true},
		// The name's pitch outranks the field, as it does for the colour.
		{"Dig In (Yellow) - FAB385", "FAB384", "1", "FAB385", true},
		// The dataset agrees: the catalog's number stands.
		{"Staunch Response (Red) (Marvel) - TNP019", "TNP019", "1", "TNP019", false},
		// The dataset cannot say which of two numbers the pitch is.
		{"Snatch (Yellow)", "WTR100", "2", "WTR100", false},
		// No pitch, no number, or a card the dataset does not print there.
		{"Dig In", "FAB384", "", "FAB384", false},
		{"Dig In (Yellow)", "", "2", "", false},
		{"Show No Mercy", "ARR013", "3", "ARR013", false},
	} {
		got, moved := idx.ownNumber(catalogProduct(t, test.name, test.number, test.pitch), test.number)
		if got != test.want || moved != test.moved {
			t.Errorf("ownNumber(%q, %q) = %q, %v; want %q, %v", test.name, test.number, got, moved, test.want, test.moved)
		}
	}
}
