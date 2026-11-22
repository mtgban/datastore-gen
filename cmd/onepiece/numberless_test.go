package main

import (
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
	tcgplayer "github.com/mtgban/go-tcgplayer"
)

// TestProductNumber pins SPECIFICATIONS §2.2 and §2.4: a product the catalog
// files with no Number publishes none, whatever its card type, and its id stem
// is the bare product id; a numberFixes product publishes its fixed number.
func TestProductNumber(t *testing.T) {
	tests := []struct {
		name     string
		product  tcgplayer.Product
		finish   string
		wantNum  string
		wantBase string
		wantID   string
	}{
		{
			"DON!! card",
			tcgplayer.Product{ProductID: 456059, Name: "DON!! Card (Nami)", ExtendedData: []tcgplayer.ExtendedField{
				{Name: "CardType", Value: "DON!!"},
			}},
			"Foil", "", "DON!! Card", "456059_foil",
		},
		{
			"promo leader",
			tcgplayer.Product{ProductID: 512345, Name: "Monkey.D.Luffy (Sealed Battle Kit Vol. 1)", ExtendedData: []tcgplayer.ExtendedField{
				{Name: "CardType", Value: "Leader"},
			}},
			"Normal", "", "Monkey.D.Luffy", "512345",
		},
		{
			"numbered card",
			tcgplayer.Product{ProductID: 453522, Name: "Roronoa Zoro - OP01-001", ExtendedData: []tcgplayer.ExtendedField{
				{Name: "Number", Value: "OP01-001"},
				{Name: "CardType", Value: "Leader"},
			}},
			"Normal", "OP01-001", "Roronoa Zoro", "op01-001_453522",
		},
		{
			"number the catalog files wrong",
			tcgplayer.Product{ProductID: 710248, Name: "Sanji (CS 26-27 Top Player Pack Vol. 1)", ExtendedData: []tcgplayer.ExtendedField{
				{Name: "Number", Value: "EB04-052"},
				{Name: "CardType", Value: "Character"},
			}},
			"Foil", "EB02-054", "Sanji", "eb02-054_710248_foil",
		},
		{
			"number the catalog files right",
			tcgplayer.Product{ProductID: 710248, Name: "Sanji (CS 26-27 Top Player Pack Vol. 1)", ExtendedData: []tcgplayer.ExtendedField{
				{Name: "Number", Value: "EB02-054"},
				{Name: "CardType", Value: "Character"},
			}},
			"Foil", "EB02-054", "Sanji", "eb02-054_710248_foil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := decompose(tt.product, numberFor(tt.product))
			if s.number != tt.wantNum {
				t.Errorf("number = %q, want %q", s.number, tt.wantNum)
			}
			if s.baseName != tt.wantBase {
				t.Errorf("baseName = %q, want %q", s.baseName, tt.wantBase)
			}
			if id := emit.IDBase(s.number, tt.product.ProductID) + emit.FinishSuffix(tt.finish); id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
		})
	}
}
