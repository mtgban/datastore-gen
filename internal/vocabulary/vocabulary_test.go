package vocabulary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckReadsTheRules pins each rule against a printing that breaks it
// and one that does not, so the rules hold without a datastore to hand.
func TestCheckReadsTheRules(t *testing.T) {
	for _, test := range []struct {
		desc      string
		printings []Printing
		want      func(Problems) int
	}{
		{
			desc:      "a token carrying a space is not one",
			printings: []Printing{{ID: "a", PromoTypes: []string{"judge pack"}}},
			want:      func(p Problems) int { return len(p.NotSlugs) },
		},
		{
			desc:      "a long token holding a set is a fold that was not made",
			printings: []Printing{{ID: "a", PromoTypes: []string{"twilightmasqueradestamped"}}},
			want:      func(p Problems) int { return len(p.TooLong) },
		},
		{
			desc:      "and a long token holding none is a name that is simply long",
			printings: []Printing{{ID: "a", PromoTypes: []string{"northamericainternationalchampionship"}}},
			want:      func(p Problems) int { return len(p.TooLong) },
		},
		{
			desc:      "a rarity is not a label",
			printings: []Printing{{ID: "a", Rarity: "Marvel", PromoTypes: []string{"marvel"}}},
			want:      func(p Problems) int { return len(p.RarityEchoes) },
		},
		{
			desc:      "nor is a finish",
			printings: []Printing{{ID: "a", Finish: "Cold Foil", PromoTypes: []string{"coldfoil"}}},
			want:      func(p Problems) int { return len(p.FinishEchoes) },
		},
		{
			desc: "and a colour tells two printings apart like any other field",
			printings: []Printing{
				{ID: "a", Facts: map[string]any{"name": "Staunch Response", "color": "Red"}},
				{ID: "b", Facts: map[string]any{"name": "Staunch Response", "color": "Yellow"}},
			},
			want: func(p Problems) int { return len(p.Alike) },
		},
		{
			desc: "two printings no field tells apart",
			printings: []Printing{
				{ID: "a", Facts: map[string]any{"name": "DON!! Card", "number": "DON"}},
				{ID: "b", Facts: map[string]any{"name": "DON!! Card", "number": "DON"}},
			},
			want: func(p Problems) int { return len(p.Alike) },
		},
		{
			desc: "and the same two once a mark says which is which",
			printings: []Printing{
				{ID: "a", Facts: map[string]any{"name": "DON!! Card", "number": "DON", "watermark": "nami"}},
				{ID: "b", Facts: map[string]any{"name": "DON!! Card", "number": "DON", "watermark": "buggy"}},
			},
			want: func(p Problems) int { return len(p.Alike) },
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			found := Check(test.printings, []string{"SV06: Twilight Masquerade", "Twilight Masquerade"})
			// The last case of each pair is the clean one, named "and".
			clean := strings.HasPrefix(test.desc, "and ")
			if got := test.want(found); (got == 0) != clean {
				t.Errorf("Check() found %d, lines %v", got, found.Lines())
			}
		})
	}
}

// TestCheckCountsOnce pins that a token breaking a rule on a hundred
// printings is one finding, not a hundred: the vocabulary is what is being
// read, and a reader has to be able to see the whole of it.
func TestCheckCountsOnce(t *testing.T) {
	var printings []Printing
	for i := range 100 {
		printings = append(printings, Printing{
			ID:         string(rune('a' + i%26)),
			Facts:      map[string]any{"number": i},
			PromoTypes: []string{"event pack"},
		})
	}
	if found := Check(printings, nil); len(found.NotSlugs) != 1 {
		t.Errorf("Check() found %d tokens, want the one", len(found.NotSlugs))
	}
}

// TestNotADatastoreSaysSo pins that a file which is not a built datastore is
// named as one rather than read as an empty vocabulary. The games whose
// upstream is a JSON document keep it under the datastore's own name, and a
// silent zero there reads as a clean bill of health.
func TestNotADatastoreSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream.json")
	if err := os.WriteFile(path, []byte(`{"pageProps":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDatastore(path); !errors.Is(err, ErrNotDatastore) {
		t.Errorf("ReadDatastore() = %v, want ErrNotDatastore", err)
	}
}

// TestFinishesAreReadFromEitherShape pins that a card saying its finishes in
// a printings array is read as the several printings it is. Read as one, a
// card's finishes all carry the same empty finish and every sibling looks
// alike.
func TestFinishesAreReadFromEitherShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.json")
	body := `{"cards":[{"id":1,"name":"Dalmatian Puppy","number":4,"setCode":3,
		"printings":[{"finish":"Normal","id":"1"},{"finish":"Cold Foil","id":"1_foil"}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	printings, err := ReadDatastore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(printings) != 2 {
		t.Fatalf("read %d printings, want 2", len(printings))
	}
	if printings[0].Facts["number"] != "4" || printings[0].Facts["setCode"] != "3" {
		t.Errorf("read number %v set %v, want the numbers said as themselves",
			printings[0].Facts["number"], printings[0].Facts["setCode"])
	}
	if found := Check(printings, nil); len(found.Alike) != 0 {
		t.Errorf("two finishes of one card read as alike: %v", found.Lines())
	}
}

// TestPrintingTokensAreHeldToTheRules pins that a token on a printing - the
// treatment lorcana moved off the card and onto the foil that has it - is
// read with the card's, so a printing publishing words rather than a slug is
// found and two printings told apart by their treatments are not alike.
func TestPrintingTokensAreHeldToTheRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.json")
	body := `{"cards":[{"id":1,"name":"Simba","number":4,"setCode":3,
		"printings":[{"finish":"Cold Foil","id":"1_foil","promoTypes":["free form"]},{"finish":"Holofoil","id":"1_holofoil","promoTypes":["rainbowpillars"]}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	printings, err := ReadDatastore(path)
	if err != nil {
		t.Fatal(err)
	}
	found := Check(printings, nil)
	if len(found.NotSlugs) != 1 || found.NotSlugs[0] != "free form" {
		t.Errorf("Check() found %v, want the printing's own token reported", found.Lines())
	}
	if len(found.Alike) != 0 {
		t.Errorf("two treatments of one card read as alike: %v", found.Lines())
	}
}
