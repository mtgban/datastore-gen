package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// TestMintEuropeanPrints pins which runs a European first print mints on the
// branches the golden does not reach.
func TestMintEuropeanPrints(t *testing.T) {
	row := func(number, name, finish, variant string, passcode int) *emit.Card {
		setCode, _, _ := strings.Cut(number, "-")
		return &emit.Card{ID: strings.ToLower(number) + emit.FinishSuffix(finish) + emit.FinishSuffix(variant),
			Name: name, SetCode: setCode, Number: number, Rarity: "Rare", Finish: finish, Variant: variant,
			ExternalLinks: emit.Links{KonamiID: passcode}}
	}
	upstream := func(passcode int, name, code string) ygoCard {
		c := ygoCard{ID: passcode, Name: name}
		c.Sets = append(c.Sets, struct {
			Code   string `json:"set_code"`
			Rarity string `json:"set_rarity"`
		}{code, "Rare"})
		return c
	}
	for _, tt := range []struct {
		name  string
		cards []*emit.Card
		ygo   ygoCard
		want  []string
	}{
		{"passcode join collects every plain run", []*emit.Card{
			row("XYZ-001", "Red-Eyes B. Dragon", "Unlimited", "", 74677422),
			row("XYZ-001", "Red-Eyes B. Dragon", "1st Edition", "", 74677422),
		}, upstream(74677422, "Red-Eyes Black Dragon", "XYZ-E001"), []string{"xyz-e001_unlimited", "xyz-e001_1stedition"}},
		{"plain row displaces a variant one and its run", []*emit.Card{
			row("XYZ-001", "Dark Magician", "Limited", "Alternate Art", 46986414),
			row("XYZ-001", "Dark Magician", "Unlimited", "", 46986414),
		}, upstream(46986414, "Dark Magician", "XYZ-E001"), []string{"xyz-e001_unlimited"}},
		{"variant row adds no run", []*emit.Card{
			row("XYZ-001", "Dark Magician", "Unlimited", "", 46986414),
			row("XYZ-001", "Dark Magician", "1st Edition", "Alternate Art", 46986414),
		}, upstream(46986414, "Dark Magician", "XYZ-E001"), []string{"xyz-e001_unlimited"}},
		{"unjoined print mints the set's default run", []*emit.Card{
			row("XYZ-002", "Mystical Elf", "1st Edition", "", 0),
			row("ABC-001", "Time Wizard", "Unlimited", "", 0),
		}, upstream(71625222, "Time Wizard", "XYZ-E005"), []string{"xyz-e005_1stedition"}},
	} {
		sets := map[string]*emit.Set{"XYZ": {}, "ABC": {}}
		var got []string
		for _, c := range mintEuropeanPrints(tt.cards, sets, []ygoCard{tt.ygo}) {
			if c.Variant == "European" {
				got = append(got, c.ID)
			}
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: European rows %q, want %q", tt.name, got, tt.want)
		}
	}
}
