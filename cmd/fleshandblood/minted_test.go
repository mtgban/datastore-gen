package main

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// TestDropMintedTwinsKeepsTheSoldCard pins that a minted entry is dropped
// when a priced entry wears its fabId, or its set, padding-blind number and
// name, and kept otherwise.
func TestDropMintedTwinsKeepsTheSoldCard(t *testing.T) {
	entry := func(id, setCode, number, name, fabID string, product int) *emit.Card {
		return &emit.Card{ID: id, SetCode: setCode, Number: number, Name: name,
			ExternalLinks: emit.Links{TCGPlayerID: product, FabID: fabID}}
	}
	kept, dropped := dropMintedTwins([]*emit.Card{
		entry("her0156", "HER", "HER0156", "Ser Boltyn", "HER156", 500),
		entry("m-her156", "HER", "HER156", "ser boltyn", "OTHER", 0),
		entry("m-hnt134", "HNT", "HNT134", "Knife Through Butter", "HER156", 0),
		entry("m-token", "HNT", "HNT200", "Gold", "HNT200", 0),
	})
	var ids []string
	for _, c := range kept {
		ids = append(ids, c.ID)
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

// TestMintedSetDateLeavesAnUndatedSetUndated pins that a minted set of promos
// the dataset gives no date, in a catalog with no promotional group, publishes
// no releaseDate, that the dataset's date and the series' still apply, and
// that the series' applies only to a set of promos.
func TestMintedSetDateLeavesAnUndatedSetUndated(t *testing.T) {
	catalog := tcgplayer.CatalogDump{Groups: []tcgplayer.Group{
		{GroupID: 3, Name: "Expansion", PublishedOn: "2020-01-01T00:00:00"},
	}}
	seriesDate := promoSeriesDate(catalog, map[int]bool{}, map[int]int{3: 5000})
	undated := fabSet{ID: "WIN", Name: "Win Pack"}
	encoded, err := json.Marshal(&emit.Set{Name: undated.Name,
		ReleaseDate: mintedSetDate("WIN", undated, true, seriesDate)})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"name":"Win Pack"}` {
		t.Errorf("undated minted set encodes as %s, want no releaseDate", encoded)
	}

	if got := mintedSetDate("WIN", undated, true, "2019-10-11"); got != "2019-10-11" {
		t.Errorf("mintedSetDate with a promo series = %q, want 2019-10-11", got)
	}
	if got := mintedSetDate("XXX", undated, false, "2019-10-11"); got != "" {
		t.Errorf("mintedSetDate for a set not wholly promos = %q, want none", got)
	}
	dated := fabSet{ID: "WIN", Printings: []struct {
		InitialReleaseDate string `json:"initial_release_date"`
	}{{InitialReleaseDate: "2021-03-05T00:00:00.000Z"}}}
	if got := mintedSetDate("WIN", dated, true, "2019-10-11"); got != "2021-03-05" {
		t.Errorf("mintedSetDate with a dataset date = %q, want 2021-03-05", got)
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
