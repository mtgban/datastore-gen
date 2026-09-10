package main

import (
	"slices"
	"testing"

	"github.com/mtgban/datastore-gen/internal/vocabulary"
)

// TestPromoTypesOf pins what a qualifier is published as. A phrase naming
// both a promotion and the product it shipped in says two things, and a
// reader asking for either has to be able to name it: the whole phrase run
// together is 38 characters, longer than any query carries and longer than
// vocabulary.TokenLimit allows a name to be.
func TestPromoTypesOf(t *testing.T) {
	for _, test := range []struct {
		qualifiers []string
		number     string
		want       []string
	}{
		// The T1 Worlds Champion run, sold as two bundles. One promotion,
		// and which bundle a copy came in beside it.
		{[]string{"T1 Worlds Champion Player Bundle"}, "T1A001",
			[]string{"t1worldschampion", "playerbundle"}},
		{[]string{"T1 Worlds Champion Signature Edition Bundle"}, "T1S001",
			[]string{"t1worldschampion", "signatureeditionbundle"}},
		{[]string{"T1 Worlds Champion Signature Edition Bundle", "Serial Numbered"}, "T1S001",
			[]string{"t1worldschampion", "signatureeditionbundle", "serialnumbered"}},
		// A qualifier saying one thing stays one token.
		{[]string{"Metal", "Best Of"}, "247", []string{"metal", "bestof"}},
		// The rules the split has to leave alone: a qualifier that only
		// repeats the number, and the " Promo" the set already says.
		{[]string{"R01c"}, "R01c", nil},
		{[]string{"Fist Bump Promo"}, "R07", []string{"fistbump"}},
	} {
		got := promoTypesOf(test.qualifiers, test.number)
		if !slices.Equal(got, test.want) {
			t.Errorf("promoTypesOf(%q, %q) = %q, want %q",
				test.qualifiers, test.number, got, test.want)
		}
		for _, token := range got {
			if len(token) > vocabulary.TokenLimit {
				t.Errorf("promoTypesOf(%q, %q) published %q, %d characters past the %d a name may read",
					test.qualifiers, test.number, token,
					len(token)-vocabulary.TokenLimit, vocabulary.TokenLimit)
			}
		}
	}
}
