package build

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/validate"

	"github.com/mtgban/go-tcgplayer"
)

// singleTypes is what the tiny catalog files its cards under.
var singleTypes = []string{"Cards"}

// tinyCatalog holds one set with a priced card, an unpriced card and a box,
// a promo group with one card, and a group holding nothing.
func tinyCatalog() tcgplayer.CatalogDump {
	return tcgplayer.CatalogDump{
		Category: tcgplayer.Category{CategoryID: 99},
		Groups: []tcgplayer.Group{
			{GroupID: 1, Name: "Alpha", Abbreviation: "AB", PublishedOn: "2026-01-02T00:00:00"},
			{GroupID: 2, Name: "Alpha Promos", Abbreviation: "ABP", PublishedOn: "2026-02-03T00:00:00"},
			{GroupID: 3, Name: "Not Yet", Abbreviation: "NY"},
		},
		Products: []tcgplayer.Product{
			{ProductID: 900, Name: "Alpha Booster Box", GroupID: 1, ProductType: "Sealed Products", ImageURL: "https://tcgplayer-cdn.tcgplayer.com/product/900_200w.jpg"},
			{ProductID: 100, Name: "Ace", GroupID: 1, ProductType: "Cards"},
			{ProductID: 101, Name: "Deuce", GroupID: 1, ProductType: "Cards"},
			{ProductID: 200, Name: "Ace (Promo)", GroupID: 2, ProductType: "Cards"},
			{ProductID: 800, Name: "Alpha Tin", GroupID: 1, ProductType: "Sealed Products"},
		},
	}
}

// tinyPrintings sells the first card in two printings, the promo in one and
// the second card in none.
var tinyPrintings = map[int][]string{
	100: {"Normal", "Foil"},
	200: {"Foil"},
}

// logged runs f and returns what it logged, without the timestamps.
func logged(t *testing.T, f func()) string {
	t.Helper()
	var buf bytes.Buffer
	flags, out := log.Flags(), log.Writer()
	log.SetFlags(0)
	log.SetOutput(&buf)
	defer func() {
		log.SetFlags(flags)
		log.SetOutput(out)
	}()
	f()
	return buf.String()
}

func TestStartParsesTheCommonFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	own := fs.String("own", "", "a builder's own upstream")
	b, err := start(Game{Category: 99}, fs, []string{
		"-own", "x", "-tcg-catalog", "c.json", "-o", "out.json", "-upstream-cache", "cache",
		"-against", "base.json", "-against-tolerance", "0.5", "-baseline-fit", "fit.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if *own != "x" || b.CatalogPath != "c.json" || b.Output != "out.json" || b.UpstreamCache != "cache" ||
		b.Against != "base.json" || b.Tolerance != 0.5 || b.BaselineFit != "fit.json" {
		t.Errorf("parsed %+v, own %q", b, *own)
	}
	if usage := fs.Lookup("tcg-catalog").Usage; !strings.Contains(usage, "category 99") {
		t.Errorf("-tcg-catalog usage %q names no category", usage)
	}
	if def := fs.Lookup("against-tolerance").DefValue; def != "0.01" {
		t.Errorf("-against-tolerance defaults to %s", def)
	}
}

func TestStartRefusesABuildWithNoCatalog(t *testing.T) {
	for _, test := range []struct {
		carries, want string
	}{
		{"", "the dump carries the printings and the ids"},
		{"the product ids", "the dump carries the product ids"},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		_, err := start(Game{Category: 99, Carries: test.carries}, fs, nil)
		if err == nil || !strings.HasSuffix(err.Error(), test.want) {
			t.Errorf("Carries %q: %v, want a refusal ending %q", test.carries, err, test.want)
		}
	}
}

func TestReadCatalog(t *testing.T) {
	data, err := json.Marshal(tinyCatalog())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	b := &Build{Game: Game{Category: 99}, CatalogPath: path}
	catalog := b.ReadCatalog()
	if len(catalog.Groups) != 3 || len(catalog.Products) != 5 {
		t.Errorf("read %d groups, %d products", len(catalog.Groups), len(catalog.Products))
	}
	got := logged(t, func() { LogCatalogf(&catalog, "; upstream: %d cards", 7) })
	if want := "catalog: 3 groups, 5 products; upstream: 7 cards\n"; got != want {
		t.Errorf("logged %q, want %q", got, want)
	}
}

func TestSplitProducts(t *testing.T) {
	catalog := tinyCatalog()
	split := SplitProducts(&catalog, singleTypes, tinyPrintings)
	ids := func(products []tcgplayer.Product) []int {
		var out []int
		for _, p := range products {
			out = append(out, p.ProductID)
		}
		return out
	}
	if split.Products != 5 {
		t.Errorf("Products = %d", split.Products)
	}
	if got := ids(split.Cards); !slices.Equal(got, []int{100, 200}) {
		t.Errorf("Cards = %v, want the priced singles in catalog order", got)
	}
	if got := ids(split.Sealed); !slices.Equal(got, []int{900, 800}) {
		t.Errorf("Sealed = %v, want everything not a single, in catalog order", got)
	}
	if want := []string{`"Deuce" (101)`}; !slices.Equal(split.Unpriced, want) {
		t.Errorf("Unpriced = %q, want %q", split.Unpriced, want)
	}
	got := logged(t, func() { split.LogUnpriced("English sku") })
	if want := "unpriced: 1 card products TCGplayer sells no English sku for yet, carried once it does: \"Deuce\" (101)\n"; got != want {
		t.Errorf("logged %q, want %q", got, want)
	}
	if got := logged(t, func() { Split{}.LogUnpriced("sku") }); got != "" {
		t.Errorf("nothing unpriced logged %q", got)
	}
}

func TestCoverage(t *testing.T) {
	catalog := tinyCatalog()
	finishes := Coverage(&catalog, singleTypes, tinyPrintings)
	if !maps.EqualFunc(finishes, tinyPrintings, slices.Equal) {
		t.Errorf("Coverage = %v, want every priced single's printings", finishes)
	}
	got := logged(t, func() { LogCoverage(1, finishes) })
	if want := "coverage: 1 of 2 catalog card products carried, 1 skipped\n"; got != want {
		t.Errorf("logged %q, want %q", got, want)
	}
}

func TestSets(t *testing.T) {
	catalog := tinyCatalog()
	codes := emit.SetCodes(catalog.Groups)
	sets, tally := Sets(&catalog, codes, func(g tcgplayer.Group) bool { return strings.Contains(g.Name, "Promos") })
	want := map[string]emit.Set{
		"AB":  {Name: "Alpha", ReleaseDate: "2026-01-02"},
		"ABP": {Name: "Alpha Promos", ReleaseDate: "2026-02-03", Type: "promo"},
	}
	if !maps.EqualFunc(sets, want, func(got *emit.Set, want emit.Set) bool { return *got == want }) {
		t.Errorf("Sets = %v, want %v", sets, want)
	}
	if tally != (SetTally{Populated: 2, Promo: 1, Empty: 1}) {
		t.Errorf("tally = %+v", tally)
	}
	logs := logged(t, func() { tally.Log(sets) })
	if want := "promotional sets: 1 of 2\nsets: 1 empty groups hold no product and are skipped\n"; logs != want {
		t.Errorf("logged %q, want %q", logs, want)
	}
}

func TestSealedEntries(t *testing.T) {
	catalog := tinyCatalog()
	split := SplitProducts(&catalog, singleTypes, tinyPrintings)
	codes := emit.SetCodes(catalog.Groups)
	sealed := SealedEntries(split.Sealed, GroupShelf(GroupsByID(catalog.Groups), codes), strings.ToLower)
	want := []emit.Sealed{
		{ID: "ab-800", Name: "Alpha Tin", SetCode: "AB", ReleaseDate: "2026-01-02", ExternalLinks: emit.Links{TCGPlayerID: 800}},
		{ID: "ab-900", Name: "Alpha Booster Box", SetCode: "AB", ReleaseDate: "2026-01-02",
			Image: emit.ImageURL("https://tcgplayer-cdn.tcgplayer.com/product/900_200w.jpg"), ExternalLinks: emit.Links{TCGPlayerID: 900}},
	}
	if !slices.EqualFunc(sealed, want, func(got *emit.Sealed, want emit.Sealed) bool { return *got == want }) {
		t.Errorf("SealedEntries = %v, want them in product id order: %v", sealed, want)
	}
}

// TestPublish builds the tiny catalog's datastore end to end and reads back
// what was written.
func TestPublish(t *testing.T) {
	catalog := tinyCatalog()
	codes := emit.SetCodes(catalog.Groups)
	split := SplitProducts(&catalog, singleTypes, tinyPrintings)
	finishes := Coverage(&catalog, singleTypes, tinyPrintings)
	sets, _ := Sets(&catalog, codes, func(tcgplayer.Group) bool { return false })
	sealed := SealedEntries(split.Sealed, GroupShelf(GroupsByID(catalog.Groups), codes), strings.ToLower)
	var cards []*emit.Card
	for _, product := range split.Cards {
		for _, finish := range tinyPrintings[product.ProductID] {
			cards = append(cards, &emit.Card{
				ID:            emit.IDBase("1", product.ProductID) + emit.FinishSuffix(finish),
				Name:          product.Name,
				Number:        "1",
				SetCode:       codes[product.GroupID],
				Rarity:        "Rare",
				Finish:        finish,
				Image:         "https://tcgplayer-cdn.tcgplayer.com/product/100_400w.jpg",
				ExternalLinks: emit.Links{TCGPlayerID: product.ProductID},
			})
		}
	}
	properties := map[string][]string{"rarity": {"Rare"}}

	output := filepath.Join(t.TempDir(), "test.json")
	b := &Build{
		Game: Game{
			Category: 99,
			Rules:    validate.Rules{Game: "test", Identity: []string{"name", "number", "setCode", "rarity", "finish"}},
		},
		Output: output,
	}
	got := logged(t, func() {
		b.Publish(&emit.Datastore{Game: "test", Properties: properties, Sets: sets, Cards: cards, Sealed: sealed},
			Contract{Finishes: finishes, Split: split, Carried: len(split.Cards)})
	})
	if want := "validated: 2 sets, 3 cards, 2 sealed\n"; !strings.HasSuffix(got, want) {
		t.Errorf("logged %q, want it to end %q", got, want)
	}

	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := emit.Unwrap(data)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Game       string              `json:"game"`
		Sets       map[string]any      `json:"sets"`
		Cards      []map[string]any    `json:"cards"`
		Sealed     []map[string]any    `json:"sealed"`
		Properties map[string][]string `json:"properties"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Game != "test" || len(doc.Sets) != 2 || len(doc.Cards) != 3 || len(doc.Sealed) != 2 ||
		!maps.EqualFunc(doc.Properties, properties, slices.Equal) {
		t.Errorf("wrote game %q, %d sets, %d cards, %d sealed, properties %v",
			doc.Game, len(doc.Sets), len(doc.Cards), len(doc.Sealed), doc.Properties)
	}
}
