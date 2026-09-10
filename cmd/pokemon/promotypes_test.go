package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestSaysFinish pins that every spelling the catalog writes a printing's
// own finish in is read as the finish, and that a spelling of some other
// finish is not.
func TestSaysFinish(t *testing.T) {
	for _, test := range []struct {
		tag, finish string
		want        bool
	}{
		{"holofoil", "Holofoil", true},
		{"holo", "Holofoil", true},
		{"holo", "1st Edition Holofoil", true},
		{"holo", "Reverse Holofoil", false},
		{"holo", "Normal", false},
		{"nonholo", "Normal", true},
		{"nonholo", "Holofoil", false},
		{"reverseholofoil", "Reverse Holofoil", true},
		{"reverseholo", "Reverse Holofoil", true},
		{"cosmosholo", "Holofoil", false},
		{"stamped", "Holofoil", false},
	} {
		if got := saysFinish(test.tag, test.finish); got != test.want {
			t.Errorf("saysFinish(%q, %q) = %v, want %v", test.tag, test.finish, got, test.want)
		}
	}
}

// setNamesOf registers set names the way main does: whole, past the era,
// and past the code.
func setNamesOf(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		out[mtgmatcherNormalize(name)] = true
		if _, rest := eraOf(name); rest != "" {
			out[mtgmatcherNormalize(rest)] = true
		}
		if _, rest, found := strings.Cut(name, ": "); found {
			out[mtgmatcherNormalize(rest)] = true
		}
	}
	return out
}

// TestSetNamedReadsPastAnEraAndNoFurther pins which words are read as an
// era in front of a set: the series the game is sold in, and not any short
// word, which read "With" as one in front of "Pokemon GO".
func TestSetNamedReadsPastAnEraAndNoFurther(t *testing.T) {
	setNames := setNamesOf("Legends Awakened", "Undaunted", "EX Ruby and Sapphire",
		"Pokemon GO", "Best of Game", "SV06: Twilight Masquerade", "Team Rocket")
	for _, test := range []struct{ label, want string }{
		{"Legends Awakened", "legendsawakened"},
		{"DP Legends Awakened", "legendsawakened"},
		{"HGSS Undaunted", "undaunted"},
		{"Ruby & Sapphire", "rubysapphire"},
		{"EX Ruby & Sapphire", "exrubysapphire"},
		{"Twilight Masquerade", "twilightmasquerade"},
		{"With Pokemon GO", ""},
		{"Best of Game Promo", ""},
		{"Rocket", ""},
		{"Team Rocket", "teamrocket"},
	} {
		if got := setNamed(test.label, setNames); got != test.want {
			t.Errorf("setNamed(%q) = %q, want %q", test.label, got, test.want)
		}
	}
}

// TestPromoTypesOfReadsTheFactsOffALabel pins, label by label, which of a
// printing's labels are a promotion and which are a fact this datastore
// publishes elsewhere - the finish, the set, the place in a deck - and so
// become the mark of the copy rather than a promo type.
func TestPromoTypesOfReadsTheFactsOffALabel(t *testing.T) {
	facts := published{
		pokemon:  map[string]bool{"charizard": true},
		setNames: setNamesOf("Best of Game", "Gym Challenge", "SM - Team Up", "Fossil", "SV08.5: Prismatic Evolutions", "XY: Evolutions"),
		tails:    map[string]bool{"Stamped": true, "Prerelease": true},
	}
	for _, test := range []struct {
		desc    string
		labels  []string
		finish  string
		onShelf bool
		own     map[string]bool
		kept    []string
		found   string
		left    []string
	}{
		{desc: "the finish the printing is priced as is a mark",
			labels: []string{"Holo"}, finish: "Holofoil", found: "holo", left: []string{"holo"}},
		{desc: "the plain finish too",
			labels: []string{"Non-Holo"}, finish: "Normal", found: "non-holo", left: []string{"non-holo"}},
		{desc: "a finish the printing is not priced as stays a label",
			labels: []string{"Holo"}, finish: "Reverse Holofoil", kept: []string{"holo"}},
		{desc: "two marks on one printing are joined",
			labels: []string{"Non-Holo", "Professor Rowan"}, finish: "Normal", found: "non-holo professor rowan", left: []string{"non-holo"}},
		{desc: "a label naming the set the card sits in restates it",
			labels: []string{"Black Star Promos"}, own: map[string]bool{"blackstarpromos": true}, left: []string{"black star promos"}},
		{desc: "a set named on a shelf is where the card came from",
			labels: []string{"Gym Challenge"}, onShelf: true, found: "gym challenge", left: []string{"gym challenge"}},
		{desc: "and off a shelf is the promotion it is named for",
			labels: []string{"Gym Challenge"}, kept: []string{"gymchallenge"}},
		{desc: "a shelf reads past the word promo",
			labels: []string{"Best of Game Promo"}, onShelf: true, found: "best of game", left: []string{"best of game"}},
		{desc: "a set in front of a promotion the catalog writes alone splits",
			labels: []string{"Team Up Stamped"}, kept: []string{"stamped"}, found: "team up", left: []string{"team up"}},
		{desc: "at exactly the limit too",
			labels: []string{"XY Evolutions Prerelease"}, kept: []string{"prerelease"}, found: "xy evolutions", left: []string{"xy evolutions"}},
		{desc: "a set in front of words that are no promotion on their own does not",
			labels: []string{"Fossil Museum Exclusive"}, kept: []string{"fossilmuseumexclusive"}},
		{desc: "past the limit the split is forced",
			labels: []string{"Prismatic Evolutions Stamped"}, kept: []string{"stamped"}, found: "prismatic evolutions", left: []string{"prismatic evolutions"}},
		{desc: "the place in a deck and the deck are one mark",
			labels: []string{"#42 Charizard Stamped"}, kept: []string{"stamped"}, found: "42 charizard", left: []string{"42", "charizard"}},
		{desc: "a copyright year is a mark and not a date",
			labels: []string{"2021 Copyright Date"}, found: "2021 copyright date", left: []string{"2021 copyright date"}},
		{desc: "a run of numbers is a numbering",
			labels: []string{"SWSH287-290"}, found: "swsh287-290", left: []string{"swsh287-290"}},
		{desc: "a card kind at its own number is a mark",
			labels: []string{"Prime"}, found: "prime"},
		{desc: "a cloak is a forme",
			labels: []string{"Plant Cloak"}, found: "plant cloak", left: []string{"plant cloak"}},
		{desc: "an instalment alone still leaves the word behind",
			labels: []string{"Series 7"}, kept: []string{"series"}, found: "series 7"},
	} {
		s := &single{}
		for _, label := range test.labels {
			s.quals = append(s.quals, qual{text: label})
		}
		kept, left, year, found, _ := promoTypesOf(s, facts, test.onShelf, test.own, test.finish, "")
		// An empty list and none are the same answer here.
		same := func(got, want []string) bool {
			return len(got) == 0 && len(want) == 0 || reflect.DeepEqual(got, want)
		}
		if !same(kept, test.kept) {
			t.Errorf("%s: promo types = %q, want %q", test.desc, kept, test.kept)
		}
		if found != test.found {
			t.Errorf("%s: mark = %q, want %q", test.desc, found, test.found)
		}
		if !same(left, test.left) {
			t.Errorf("%s: left out = %q, want %q", test.desc, left, test.left)
		}
		if year != "" {
			t.Errorf("%s: year = %q, want none", test.desc, year)
		}
	}
}
