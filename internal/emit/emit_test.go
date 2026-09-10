package emit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// TestFinishSuffixIsDerived pins that nothing about a category's printings
// is written here. TCGplayer names them, adds to them and renames them, and
// every one has to reach an id without a release: the plain printing takes
// the bare id and every other takes its own name.
func TestFinishSuffixIsDerived(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		// The one convention: TCGplayer calls a plain printing "Normal"
		// in every category.
		{"Normal", ""},
		{"normal", ""},
		{"Foil", "_foil"},
		{"Holofoil", "_holofoil"},
		{"Reverse Holofoil", "_reverseholofoil"},
		{"1st Edition", "_1stedition"},
		{"Cold Foil", "_coldfoil"},
		// A printing the category has yet to grow
		{"Prismatic Foil", "_prismaticfoil"},
		{"", ""},
	} {
		if got := FinishSuffix(test.in); got != test.want {
			t.Errorf("FinishSuffix(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestPlainPrintingComesFromTheCatalog pins that the printing taking the
// bare id is read off the dump, and that a category selling none - Yu-Gi-Oh
// prices by print run and calls no printing plain - says so rather than
// inventing one.
func TestPlainPrintingComesFromTheCatalog(t *testing.T) {
	with := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{Name: "Holofoil"}, {Name: "Normal"},
	}}
	if got := PlainPrinting(with); got != "Normal" {
		t.Errorf("PlainPrinting = %q, want Normal", got)
	}
	without := &tcgplayer.CatalogDump{Printings: []tcgplayer.Printing{
		{Name: "1st Edition"}, {Name: "Unlimited"},
	}}
	if got := PlainPrinting(without); got != "" {
		t.Errorf("PlainPrinting = %q, want none", got)
	}
}

// TestOrderedFinishesFollowsTheCatalog pins that a product's entries come
// out in the order TCGplayer displays the category's printings, with the
// name settling a tie.
func TestOrderedFinishesFollowsTheCatalog(t *testing.T) {
	rank := map[string]int{"Normal": 1, "Holofoil": 2, "Alpha Foil": 2, "Prismatic Foil": 9}
	got := OrderedFinishes([]string{"Prismatic Foil", "Holofoil", "Alpha Foil", "Normal"}, rank)
	want := []string{"Normal", "Alpha Foil", "Holofoil", "Prismatic Foil"}
	if !slices.Equal(got, want) {
		t.Errorf("OrderedFinishes = %v, want %v", got, want)
	}
}

// TestPromoSlugKeepsLettersAndDigits pins the one spelling a token has: lower
// case, letters and digits, nothing else - the same spelling a finish takes
// in an id, which the builders used to spell with a regular expression and
// a rune filter respectively.
func TestPromoSlugKeepsLettersAndDigits(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"Pokemon Center Exclusive", "pokemoncenterexclusive"},
		{"Non-Holo", "nonholo"},
		{"SWSH287-290", "swsh287290"},
		{"Rocket's Secret Machine", "rocketssecretmachine"},
		{"Pokémon GO", "pokmongo"},
		{"", ""},
	} {
		if got := PromoSlug(test.in); got != test.want {
			t.Errorf("PromoSlug(%q) = %q, want %q", test.in, got, test.want)
		}
		if got := FinishSlug(test.in); got != test.want {
			t.Errorf("FinishSlug(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestPlainQuotesWalksTheWholeDocument pins that the typographic quotes are
// rewritten wherever they sit, in place, and that nothing else is touched.
func TestPlainQuotesWalksTheWholeDocument(t *testing.T) {
	doc := map[string]any{
		"name": "Rocket’s Hitmonchan",
		"cards": []any{
			map[string]any{"rarity": "Ultra Pharaoh’s Rare", "number": 7},
			"Eustass“Captain”Kid",
		},
	}
	want := map[string]any{
		"name": "Rocket's Hitmonchan",
		"cards": []any{
			map[string]any{"rarity": "Ultra Pharaoh's Rare", "number": 7},
			`Eustass"Captain"Kid`,
		},
	}
	if got := PlainQuotes(doc); !reflect.DeepEqual(got, want) {
		t.Errorf("PlainQuotes = %v, want %v", got, want)
	}
}

// TestImageURLTakesTheLargerRendition pins the one substitution, made once.
func TestImageURLTakesTheLargerRendition(t *testing.T) {
	in := "https://tcgplayer-cdn.tcgplayer.com/product/42382_200w.jpg"
	if got, want := ImageURL(in), "https://tcgplayer-cdn.tcgplayer.com/product/42382_400w.jpg"; got != want {
		t.Errorf("ImageURL = %q, want %q", got, want)
	}
	if got := ImageURL("plain.jpg"); got != "plain.jpg" {
		t.Errorf("ImageURL(plain.jpg) = %q", got)
	}
}

// TestFetchReadsAFileOrTheWire pins the two sides of a location: a path is
// read as it is, a URL is fetched as this build, named, and a status other
// than 200 is an error naming the location.
func TestFetchReadsAFileOrTheWire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.json")
	if err := os.WriteFile(path, []byte(`{"held":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Fetch(path); err != nil || string(got) != `{"held":true}` {
		t.Errorf("Fetch(file) = %q, %v", got, err)
	}
	var agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"live":true}`))
	}))
	defer server.Close()
	if got, err := Fetch(server.URL + "/list"); err != nil || string(got) != `{"live":true}` {
		t.Errorf("Fetch(url) = %q, %v", got, err)
	}
	if !strings.HasPrefix(agent, "datastore-gen/") {
		t.Errorf("fetched as %q, want this build named", agent)
	}
	if _, err := Fetch(server.URL + "/missing"); err == nil || !strings.HasSuffix(err.Error(), ": HTTP 404") {
		t.Errorf("Fetch(missing) = %v, want the status named", err)
	}
}

// TestStringsOfReadsBothShapes pins the list before and after encoding, and
// that an empty string in it is nothing.
func TestStringsOfReadsBothShapes(t *testing.T) {
	if got := StringsOf([]string{"a", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("StringsOf([]string) = %v", got)
	}
	if got := StringsOf([]any{"a", "", 3, "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("StringsOf([]any) = %v", got)
	}
	if got := StringsOf("a"); got != nil {
		t.Errorf("StringsOf(string) = %v, want nil", got)
	}
}
