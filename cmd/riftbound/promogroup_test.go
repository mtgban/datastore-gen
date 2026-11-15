package main

import (
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestIsPromoGroup pins which groups are typed promo: the ones whose name
// says so, and one whose every sealed product is a promotional bundle. A main
// set stays a main set whatever bundle or promo pack it sells beside its
// boosters.
func TestIsPromoGroup(t *testing.T) {
	const sealed, card = "Sealed Products", "Cards"
	type product struct{ name, productType string }
	for _, tt := range []struct {
		group    string
		products []product
		want     bool
	}{
		{"Riftbound Organized Play Promotional Cards", nil, true},
		{"T1 2025 Worlds Champion Bundles", nil, true},
		{"Riftbound x K/DA: Couture Collection", []product{
			{"Riftbound x K/DA: Couture Collection (Sealed Promotional Bundle)", sealed},
			{"Ahri, Nine-Tailed Fox", card},
		}, true},
		{"Radiance", []product{
			{"Radiance - Nexus Night Promo Pack", sealed},
			{"Body Rune (R04b)", card},
		}, false},
		{"Tidecaller", []product{
			{"Tidecaller - Booster Box", sealed},
			{"Tidecaller - Prerelease Promotional Bundle", sealed},
		}, false},
		{"Unleashed", []product{{"Enthusiastic Promoter", card}}, false},
		{"Secret Garden", []product{{"Ultrasoft Poro", card}}, false},
	} {
		var products []tcgplayer.Product
		for _, p := range tt.products {
			products = append(products, tcgplayer.Product{Name: p.name, ProductType: p.productType})
		}
		got := isPromoGroup(tcgplayer.Group{Name: tt.group}, products)
		if got != tt.want {
			t.Errorf("isPromoGroup(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
}
