package main

import (
	"slices"
	"testing"
)

// TestMintClaims pins that a number's base printing takes the number, that a
// parallel mints on gcg-api's own id only with a rarity this build spells and
// the number does not already wear, and that the sort puts the base first.
func TestMintClaims(t *testing.T) {
	rows := []gcgCard{
		{ID: "RP-068_p1", Number: "RP-068", Rarity: "P +"},
		{ID: "GD01-013_p1", Number: "GD01-013", Rarity: "C"},
		{ID: "RP-068", Number: "RP-068", Rarity: "P"},
		{ID: "GD01-013", Number: "GD01-013", Rarity: "C"},
		{ID: "GD01-005_p1", Number: "GD01-005", Rarity: "R +"},
		{ID: "GD01-005", Number: "GD01-005", Rarity: "R"},
	}
	sortUpstream(rows)
	claimed := mints{ids: map[string]bool{}, rarities: map[string][]string{}}
	var ids []string
	for _, u := range rows {
		rarity, spelled := spellRarity(u.Rarity)
		ids = append(ids, u.ID+"="+claimed.claim(u, rarity, spelled))
	}
	want := []string{
		"GD01-005=gd01-005", "GD01-005_p1=",
		"GD01-013=gd01-013", "GD01-013_p1=",
		"RP-068=rp-068", "RP-068_p1=rp-068-p1",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("claims %q, want %q", ids, want)
	}
}
