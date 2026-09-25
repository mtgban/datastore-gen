package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// linksCard is one upstream card as decodeCard would receive it, carrying
// the id-space fields fixExternalLinks reads and corrects. 0 omits an id.
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

// TestFixExternalLinksCorrectsTheKnownRow pins the case handExternalLinks
// exists for: Vaiana (1663) carries Moana's (1433) own cardmarketId and
// cardTraderId, straight from upstream. The row corrects Vaiana's
// cardmarketId to its own Cardmarket product and drops the borrowed
// cardTraderId - CardTrader names no blueprint of its own for it - and
// leaves Moana, the id it is not keyed on, untouched.
func TestFixExternalLinksCorrectsTheKnownRow(t *testing.T) {
	cards := decodeCards(t,
		linksCard(1433, "Moana - Adventurer of Land and Sea", 801862, 311906),
		linksCard(1663, "Vaiana - Adventurer of Land and Sea", 801862, 311906),
	)
	reports := fixExternalLinks(cards)
	for _, want := range []string{"corrected from 801862 to 804164", "311906 dropped"} {
		if !slices.ContainsFunc(reports, func(r string) bool { return strings.Contains(r, want) }) {
			t.Errorf("fixExternalLinks reported %v, want a line containing %q", reports, want)
		}
	}

	vaiana := cards[1].links
	if got, _ := vaiana["cardmarketId"].(float64); got != 804164 {
		t.Errorf("Vaiana cardmarketId = %v, want 804164", vaiana["cardmarketId"])
	}
	if _, has := vaiana["cardTraderId"]; has {
		t.Error("Vaiana still carries a cardTraderId; CardTrader has no blueprint for it")
	}
	if _, has := vaiana["cardTraderUrl"]; has {
		t.Error("Vaiana still carries a cardTraderUrl")
	}

	moana := cards[0].links
	if got, _ := moana["cardmarketId"].(float64); got != 801862 {
		t.Errorf("Moana cardmarketId = %v, want its own 801862 untouched", moana["cardmarketId"])
	}
	if got, _ := moana["cardTraderId"].(float64); got != 311906 {
		t.Errorf("Moana cardTraderId = %v, want its own 311906 untouched", moana["cardTraderId"])
	}
}

// TestFixExternalLinksStandsDownWhenUpstreamAgrees pins the other half of
// the contract: where a card no longer carries the id a row expects to
// find (upstream fixed it, or dropped it), the row is reported rather than
// applied, so a fix LorcanaJSON ships itself is never overwritten and a
// field it has already dropped is never asserted to have held something
// else.
func TestFixExternalLinksStandsDownWhenUpstreamAgrees(t *testing.T) {
	// Upstream already gives Vaiana its own cardmarketId and carries no
	// cardTraderId for it at all - both halves of the row are stale.
	cards := decodeCards(t,
		linksCard(1663, "Vaiana - Adventurer of Land and Sea", 804164, 0),
	)
	stale := fixExternalLinks(cards)
	if len(stale) != 2 {
		t.Fatalf("fixExternalLinks reported %d rows, want 2 (cardmarketId already correct, no cardTraderId to drop): %v",
			len(stale), stale)
	}

	if got, _ := cards[0].links["cardmarketId"].(float64); got != 804164 {
		t.Errorf("cardmarketId changed to %v; a stood-down row must leave it alone", cards[0].links["cardmarketId"])
	}
}

// TestFixExternalLinksReportsAMissingCard pins the third case: a row whose
// card this build's upstream does not carry at all is reported, not
// silently skipped.
func TestFixExternalLinksReportsAMissingCard(t *testing.T) {
	cards := decodeCards(t,
		linksCard(1433, "Moana - Adventurer of Land and Sea", 801862, 311906),
	)
	stale := fixExternalLinks(cards)
	if len(stale) != 1 || !strings.Contains(stale[0], "1663") {
		t.Errorf("fixExternalLinks reported %v, want one report naming 1663", stale)
	}
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
