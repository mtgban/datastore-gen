package main

import (
	"maps"
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestDropMintedTwinsKeepsTheSoldCard pins that a minted entry is dropped
// when a priced entry wears its fabId, or its set, padding-blind number and
// name, and kept otherwise.
func TestDropMintedTwinsKeepsTheSoldCard(t *testing.T) {
	entry := func(id, setCode, number, name, fabID string, product int) map[string]any {
		links := map[string]any{"fabId": fabID}
		if product != 0 {
			links["tcgPlayerId"] = product
		}
		return map[string]any{"id": id, "setCode": setCode, "number": number, "name": name, "externalLinks": links}
	}
	kept, dropped := dropMintedTwins([]any{
		entry("her0156", "HER", "HER0156", "Ser Boltyn", "HER156", 500),
		entry("m-her156", "HER", "HER156", "ser boltyn", "OTHER", 0),
		entry("m-hnt134", "HNT", "HNT134", "Knife Through Butter", "HER156", 0),
		entry("m-token", "HNT", "HNT200", "Gold", "HNT200", 0),
	})
	var ids []string
	for _, raw := range kept {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	if !slices.Equal(ids, []string{"her0156", "m-token"}) || len(dropped) != 2 {
		t.Errorf("kept %v, dropped %q; want the two twins dropped", ids, dropped)
	}
}

// TestPromoSeriesDate pins that the promo series is the promotional group
// holding the most products, not the earliest one or a larger plain set.
func TestPromoSeriesDate(t *testing.T) {
	catalog := tcgplayer.CatalogDump{Groups: []tcgplayer.Group{
		{GroupID: 1, Name: "Welcome Deck", PublishedOn: "2019-08-31T00:00:00"},
		{GroupID: 2, Name: "Promo Cards", PublishedOn: "2019-10-11T00:00:00"},
		{GroupID: 3, Name: "Expansion", PublishedOn: "2020-01-01T00:00:00"},
	}}
	promoted := map[int]bool{1: true, 2: true}
	productsIn := map[int]int{1: 12, 2: 1235, 3: 5000}
	if got := promoSeriesDate(catalog, promoted, productsIn); got != "2019-10-11" {
		t.Errorf("promoSeriesDate = %q, want 2019-10-11", got)
	}
	if got := promoSeriesDate(catalog, map[int]bool{}, productsIn); got != "" {
		t.Errorf("promoSeriesDate with no promotional group = %q, want none", got)
	}
}

func TestPromoOnlySets(t *testing.T) {
	mintable := map[string][]fabRow{
		"WIN001": {{SetID: "WIN", Rarity: "P"}},
		"WIN002": {{SetID: "WIN", Rarity: "P"}},
		"MIX001": {{SetID: "MIX", Rarity: "P"}},
		"MIX002": {{SetID: "MIX", Rarity: "R"}},
		"UNK001": {{SetID: "UNK", Rarity: "X"}},
	}
	got := promoOnlySets(mintable)
	want := map[string]bool{"WIN": true}
	if !maps.Equal(got, want) {
		t.Errorf("promoOnlySets = %v, want %v", got, want)
	}
}
