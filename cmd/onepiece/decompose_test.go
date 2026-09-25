package main

import (
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
