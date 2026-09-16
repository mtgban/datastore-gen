package main

import "testing"

// A Lorcana number names one card only beside the total printed with it: a
// set's promos are numbered from 1 alongside the set's own cards, so set 1
// prints both "1/204" (Ariel - On Human Legs) and "1/P1" (Mickey Mouse -
// Brave Little Tailor). Reading the number and dropping the denominator put
// 155 pairs of unrelated cards on one (set, number).

// TestPrintedTotal pins where the denominator is read from. Upstream writes
// it twice and the two never disagree over the 185 promos that carry both,
// which is what lets promoGrouping stand in for the seven identifiers that
// spell the promo number last.
func TestPrintedTotal(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  map[string]any
		want string
	}{{
		name: "a card of the set, from the identifier's head",
		raw:  map[string]any{"fullIdentifier": "1/204 • EN • 1"},
		want: "204",
	}, {
		// Set 2 prints 207, and the Quest sets 31 and 35: the total is
		// the set's own fact and is never assumed from the biggest set.
		name: "a set that prints another size",
		raw:  map[string]any{"fullIdentifier": "205/207 • EN • 2"},
		want: "207",
	}, {
		name: "a promo, from the grouping and the head alike",
		raw:  map[string]any{"fullIdentifier": "8/P1 • EN • 1", "promoGrouping": "P1"},
		want: "P1",
	}, {
		// Seven identifiers spell the promo number after the language
		// and leave the head with no denominator at all. The grouping
		// is read first so these are not silently left without one.
		name: "a promo whose identifier spells the number last",
		raw:  map[string]any{"fullIdentifier": "1 TFC • EN • 1/P1", "promoGrouping": "P1"},
		want: "P1",
	}, {
		name: "a grouping that is not a number",
		raw:  map[string]any{"fullIdentifier": "1/CC1 • EN • 6", "promoGrouping": "CC1"},
		want: "CC1",
	}, {
		// A minted card carries neither field: its own total came from
		// the catalog's Number, and this must not overwrite it.
		name: "a minted card, which publishes neither",
		raw:  map[string]any{"fullName": "Azurite Sea Puzzle Insert (Top Left)"},
		want: "",
	}} {
		t.Run(test.name, func(t *testing.T) {
			if got := printedTotal(test.raw); got != test.want {
				t.Errorf("printedTotal(%v) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

// TestMintedNumber pins what the catalog's own Number parses to. An empty
// one parses to 0, which is why the caller asks whether the catalog wrote a
// number at all rather than trusting the parse: 173 products the catalog
// files with none were filed on number 0, one crowd per set.
func TestMintedNumber(t *testing.T) {
	for _, test := range []struct {
		in      string
		num     int
		variant string
	}{
		// A promo shelf numbers within itself, with no denominator
		{"23", 23, ""},
		// The catalog writes the set's cards as the card prints them
		{"118/204", 118, ""},
		// "Bruno Madrigal" really is numbered 0, and keeps it
		{"0/204", 0, ""},
		// A puzzle insert: no number, and the zero is the parse, not
		// the card
		{"", 0, ""},
	} {
		num, variant := mintedNumber(test.in)
		if num != test.num || variant != test.variant {
			t.Errorf("mintedNumber(%q) = (%d, %q), want (%d, %q)",
				test.in, num, variant, test.num, test.variant)
		}
	}
}
