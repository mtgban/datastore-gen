package main

import (
	"slices"
	"testing"

	tcgplayer "github.com/mtgban/go-tcgplayer"
)

// TestDecomposeToleratesTailWhitespace pins two malformed names and one
// correct control against a number tail wider than " - "+num, which the
// old exact ReplaceAll left in the published name.
func TestDecomposeToleratesTailWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		product  string
		num      string
		wantBase string
	}{
		{"no space before number", "Jewelry Bonney -PRB02-004", "PRB02-004", "Jewelry Bonney"},
		{"double space and trailing paren", "Trafalgar Law -  P-088 (Reprint)", "P-088", "Trafalgar Law"},
		{"single space, unaffected", "Yamato - OP16-098", "OP16-098", "Yamato"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decompose(tcgplayer.Product{Name: tt.product}, tt.num).baseName
			if got != tt.wantBase {
				t.Errorf("decompose(%q, %q).baseName = %q, want %q", tt.product, tt.num, got, tt.wantBase)
			}
		})
	}
}

// TestDecomposeCutsJoinedLabel pins the two DON!! products the catalog names
// with their label joined on by " // " instead of in parentheses: the label
// leaves the name, the promotion folds to the slug its other printings wear,
// and the design stays the mark it is.
func TestDecomposeCutsJoinedLabel(t *testing.T) {
	for _, tt := range []struct {
		product, wantQual string
		wantKept          []string
	}{
		{"DON!! Card // One Piece Film RED Promo", "One Piece Film RED Promo", []string{"onepiecefilmred"}},
		{"DON!! Card // Green Compass", "Green Compass", nil},
	} {
		got := decompose(tcgplayer.Product{Name: tt.product}, "")
		if got.baseName != donCardName || !slices.Equal(got.quals, []string{tt.wantQual}) {
			t.Errorf("decompose(%q) = %q %q, want %q [%q]", tt.product, got.baseName, got.quals, donCardName, tt.wantQual)
		}
		kept, _, _, _, _, _ := promoTypesOf(got.baseName, "DON!!", got.quals, nil)
		if !slices.Equal(kept, tt.wantKept) {
			t.Errorf("promoTypesOf(%q) kept %q, want %q", tt.wantQual, kept, tt.wantKept)
		}
	}
}
