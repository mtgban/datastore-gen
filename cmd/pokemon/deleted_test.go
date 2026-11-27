package main

import (
	"slices"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/go-tcgplayer"
)

// TestMintDeleted pins the minting of the deleted Japanese products: a
// deleted id the catalog lists again is left to it, a row whose group is
// gone is skipped, and an emptied group gets its set published once.
func TestMintDeleted(t *testing.T) {
	groups := []tcgplayer.Group{
		{GroupID: 1861, Name: "SM Promos", Abbreviation: "SMP"},
		{GroupID: 23095, Name: "Ash vs Team Rocket Deck Kit (JP Exclusive)", Abbreviation: "AVTR", PublishedOn: "2017-04-21T00:00:00"},
	}
	setCodes := map[int]string{1861: "SMP", 23095: "AVTR"}
	sets := map[string]*emit.Set{"SMP": {Name: "SM Promos"}}
	live := map[int]bool{494025: true} // Shaymin 225/SM-P sold again

	cards := mintDeleted(groups, setCodes, live, sets, nil, &printedTypes{})

	ids := map[int]*emit.Card{}
	for _, card := range cards {
		if former := card.ExternalLinks.FormerTCGPlayerIDs; len(former) == 1 {
			ids[former[0]] = card
		}
	}
	if _, minted := ids[494025]; minted {
		t.Error("a deleted id the catalog lists again was minted")
	}
	if _, minted := ids[489917]; minted {
		t.Error("a row whose group the catalog no longer lists was minted")
	}
	red := ids[489919]
	if red == nil || red.ID != "270-sm-p_489919_holofoil" || red.Number != "270" || red.Total != "SM-P" ||
		red.SetCode != "SMP" || red.ExternalLinks.TCGPlayerID != 0 {
		t.Errorf("Red's Pikachu = %+v, want 270/SM-P in SMP under 270-sm-p_489919_holofoil, naming no product", red)
	}
	deck := 0
	for _, card := range cards {
		if card.SetCode == "AVTR" {
			deck++
		}
		if card.Language != "Japanese" {
			t.Errorf("%s %s/%s is in %q, want Japanese", card.Name, card.Number, card.Total, card.Language)
		}
	}
	if deck != 21 {
		t.Errorf("%d Deck Kit cards minted, want 21", deck)
	}
	if set := sets["AVTR"]; set == nil || set.Name != groups[1].Name || set.ReleaseDate != "2017-04-21" || len(sets) != 2 {
		t.Errorf("sets = %v, want SMP and the Deck Kit's own, dated from its group", sets)
	}
}

// TestLinkMerged pins the linking of merged duplicates: every finish of the
// survivor names the deleted id once, and a deleted id listed again is not
// linked.
func TestLinkMerged(t *testing.T) {
	holo := &emit.Card{ID: "169_653353_holofoil", ExternalLinks: emit.Links{TCGPlayerID: 653353}}
	reverse := &emit.Card{ID: "55-100_86835_reverseholofoil", ExternalLinks: emit.Links{TCGPlayerID: 86835}}
	plain := &emit.Card{ID: "55-100_86835", ExternalLinks: emit.Links{TCGPlayerID: 86835}}
	charcadet := &emit.Card{ID: "022_666538_holofoil", ExternalLinks: emit.Links{TCGPlayerID: 666538}}

	linkMerged(map[int]bool{664093: true}, []*emit.Card{holo, reverse, plain, charcadet})

	for _, test := range []struct {
		card *emit.Card
		want []int
	}{
		{holo, []int{611828}},
		{reverse, []int{498051}},
		{plain, []int{498051}},
		{charcadet, nil},
	} {
		if got := test.card.ExternalLinks.FormerTCGPlayerIDs; !slices.Equal(got, test.want) {
			t.Errorf("%s former ids = %v, want %v", test.card.ID, got, test.want)
		}
	}
}
