package main

import (
	"strings"
	"testing"

	"github.com/mtgban/go-cardmarket"
	"github.com/mtgban/go-tcgplayer"
)

// linksCard is one upstream card as decodeCard would receive it, carrying
// the id-space fields the Cardmarket links read. 0 omits an id.
func linksCard(id int, name string, cardmarketID, cardTraderID int) map[string]any {
	links := map[string]any{}
	if cardmarketID != 0 {
		links["cardmarketId"] = float64(cardmarketID)
	}
	if cardTraderID != 0 {
		links["cardTraderId"] = float64(cardTraderID)
		links["cardTraderUrl"] = "https://www.cardtrader.com/cards/placeholder"
	}
	return map[string]any{
		"id":            float64(id),
		"fullName":      name,
		"setCode":       "7",
		"number":        float64(26),
		"externalLinks": links,
	}
}

// decodeCards runs linksCard fixtures through decodeCard the way main does,
// failing the test rather than the build if a fixture is malformed.
func decodeCards(t *testing.T, items ...map[string]any) []card {
	t.Helper()
	var cards []card
	for _, item := range items {
		c, ok := decodeCard(item)
		if !ok {
			t.Fatalf("decodeCard rejected a fixture: %v", item)
		}
		cards = append(cards, c)
	}
	return cards
}

// TestMintedCardmarketLinksBucky pins the row mintedCardmarketIDs exists
// for: Bucky's errata (597095) is minted, no upstream card carries 800381,
// so the minted card is given it.
func TestMintedCardmarketLinksBucky(t *testing.T) {
	cards := decodeCards(t, linksCard(289, "Bucky - Squirrel Squeak Tutor", 737800, 268366))
	linked, reports := mintedCardmarket([]tcgplayer.Product{{ProductID: 597095}}, cards)
	if linked[597095] != 800381 || len(linked) != 1 {
		t.Errorf("mintedCardmarket linked %v, want only 597095 -> 800381", linked)
	}
	if len(reports) != 1 || !strings.Contains(reports[0], "given cardmarketId 800381") {
		t.Errorf("mintedCardmarket reported %v, want the one row applied", reports)
	}
}

// TestMintedCardmarketStandsDown pins the two ways the row goes stale: the
// product is no longer minted (upstream publishes the card, which carries
// its own id), or an upstream card already carries the id, which a second
// claim would keep the loader from indexing for either.
func TestMintedCardmarketStandsDown(t *testing.T) {
	linked, reports := mintedCardmarket(nil, nil)
	if len(linked) != 0 || len(reports) != 1 || !strings.Contains(reports[0], "not minted") {
		t.Errorf("with 597095 not minted: linked %v, reported %v", linked, reports)
	}

	cards := decodeCards(t, linksCard(3500, "Bucky - Squirrel Squeak Tutor", 800381, 0))
	linked, reports = mintedCardmarket([]tcgplayer.Product{{ProductID: 597095}}, cards)
	if len(linked) != 0 || len(reports) != 1 || !strings.Contains(reports[0], "stands down") {
		t.Errorf("with 800381 carried upstream: linked %v, reported %v", linked, reports)
	}
}

// panoramaCatalog is Snow White - Merry as the Morning as Cardmarket files it:
// the V.1 the row's cardmarketId names and the Panorama as V.2, beside another
// card at the same number that the name keeps apart.
func panoramaCatalog(extra ...cardmarket.CatalogProduct) *cardmarket.Catalog {
	products := map[int]cardmarket.CatalogProduct{
		885615: {ExpansionID: 6551, Name: "Snow White - Merry as the Morning (V.1)", Number: "37", Version: 1},
		885830: {ExpansionID: 6551, Name: "Snow White - Merry as the Morning (V.2)", Number: "37", Version: 2},
		999001: {ExpansionID: 6551, Name: "Another Card (V.2)", Number: "37", Version: 2},
	}
	for i, product := range extra {
		products[999100+i] = product
	}
	return &cardmarket.Catalog{Data: cardmarket.CatalogData{Products: products}}
}

// panoramaCard is the row with its Panorama recorded as a TCGplayer extra.
func panoramaCard(t *testing.T, cardmarketID int) card {
	t.Helper()
	c := decodeCards(t, linksCard(2752, "Snow White - Merry as the Morning", cardmarketID, 0))[0]
	c.links["tcgPlayerExtraIds"] = []int{692015}
	return c
}

// TestFoilCardmarketIDsFindsThePanorama pins the rule: the one later version
// under the card's own name at its number, and nothing for a card whose foil
// TCGplayer does not sell apart.
func TestFoilCardmarketIDsFindsThePanorama(t *testing.T) {
	plain := decodeCards(t, linksCard(2753, "Dopey - Drawn to Music", 885615, 0))[0]
	found, reports := foilCardmarketIDs(panoramaCatalog(), []card{plain, panoramaCard(t, 885615)})
	if len(found) != 1 || found[1] != 885830 || len(reports) != 0 {
		t.Errorf("found %v, reported %v; want only card 1 -> 885830", found, reports)
	}
}

// TestFoilCardmarketIDsReportsWhatItCannotAnswer pins every way the rule
// declines, each with a report and no id.
func TestFoilCardmarketIDsReportsWhatItCannotAnswer(t *testing.T) {
	misprint := cardmarket.CatalogProduct{ExpansionID: 6551, Name: "Snow White - Merry as the Morning (V.3)", Number: "37", Version: 3}
	tests := []struct {
		desc    string
		catalog *cardmarket.Catalog
		cards   []card
		want    string
	}{
		{"not in the catalog", panoramaCatalog(), []card{panoramaCard(t, 123)}, "not in the catalog"},
		{"the row names the V.2", panoramaCatalog(), []card{panoramaCard(t, 885830)}, "0 later versions"},
		{"a misprint beside the V.2", panoramaCatalog(misprint), []card{panoramaCard(t, 885615)}, "2 later versions"},
		{"the V.2 is another card's", panoramaCatalog(), []card{panoramaCard(t, 885615),
			decodeCards(t, linksCard(9999, "Someone Else", 885830, 0))[0]}, "another card's cardmarketId"},
	}
	for _, tt := range tests {
		found, reports := foilCardmarketIDs(tt.catalog, tt.cards)
		if len(found) != 0 || len(reports) != 1 || !strings.Contains(reports[0], tt.want) {
			t.Errorf("%s: found %v, reported %v; want no id and a report containing %q", tt.desc, found, reports, tt.want)
		}
	}
}
