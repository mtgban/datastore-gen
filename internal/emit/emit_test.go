package emit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mtgban/go-tcgplayer"
)

// sameJSON reports whether two decoded documents encode to the same bytes;
// encoding/json writes map keys in sorted order.
func sameJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

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
	if got := PlainQuotes(doc); !sameJSON(got, want) {
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

// TestEnvelopeShape pins the shape every builder now publishes: meta names
// the build date and the schema version and nothing else, and data carries
// the payload exactly as given - encoding the result and reading "data"
// back off it is how every dual-shape reader in this migration finds it.
// meta is encoded first, which is why Envelope returns a struct: a reader
// that wants the version before decoding fourteen megabytes has to meet it
// first, and a map would sort "data" ahead of it.
func TestEnvelopeShape(t *testing.T) {
	got := Envelope("2026-09-14", map[string]any{"sets": "x"})

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Meta struct {
			Date    string `json:"date"`
			Version string `json:"version"`
		} `json:"meta"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Meta.Date != "2026-09-14" || decoded.Meta.Version != SchemaVersion {
		t.Errorf("meta = %+v", decoded.Meta)
	}
	// meta leads, so a reader meets the version before the payload.
	if !bytes.HasPrefix(encoded, []byte(`{"meta":`)) {
		t.Errorf("document opens %.24q, want it to open with meta", encoded)
	}
	if !sameJSON(decoded.Data, map[string]any{"sets": "x"}) {
		t.Errorf("data = %v, want the payload unchanged", decoded.Data)
	}
}

// TestTodayIsYYYYMMDD pins the one shape meta.date may take.
func TestTodayIsYYYYMMDD(t *testing.T) {
	got := Today()
	if _, err := time.Parse("2006-01-02", got); err != nil {
		t.Errorf("Today() = %q: %v", got, err)
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

// TestUnwrapRefusesAnythingButAnEnvelope holds the discriminator to "meta
// and data, both objects", and refuses every other document now that no
// datastore is published bare: a decoy "data" key, a null or a list under
// it, meta alone, and the bare shapes the games published before the
// envelope, Riftbound's and Lorcana's among them.
func TestUnwrapRefusesAnythingButAnEnvelope(t *testing.T) {
	got, err := Unwrap([]byte(`{"meta":{"date":"2026-09-14","version":"1"},"data":{"game":"pokemon"}}`))
	if err != nil || string(got) != `{"game":"pokemon"}` {
		t.Errorf("an envelope: Unwrap = %s, %v", got, err)
	}
	for _, document := range []string{
		`{"game":"pokemon","sets":{}}`,
		`{"game":"pokemon","data":{"x":1}}`,
		`{"meta":{"version":"1"},"game":"x"}`,
		`{"meta":{"version":"1"},"data":null}`,
		`{"meta":{"version":"1"},"data":"nope"}`,
		`{"meta":{"version":"1"},"data":[1]}`,
		`{"__N_SSG":true,"pageProps":{"page":{}}}`,
		`{"metadata":{"formatVersion":"2"},"cards":[]}`,
	} {
		if _, err := Unwrap([]byte(document)); !errors.Is(err, ErrNotEnvelope) {
			t.Errorf("%s: Unwrap error = %v, want ErrNotEnvelope", document, err)
		}
	}
}

// TestUnwrapRefusesAnotherSchema is what meta.version is written first for:
// a version this build does not know is refused, not decoded on the chance
// that data still reads.
func TestUnwrapRefusesAnotherSchema(t *testing.T) {
	document := []byte(`{"meta":{"date":"2026-09-14","version":"2"},"data":{"game":"pokemon"}}`)
	if _, err := Unwrap(document); !errors.Is(err, ErrUnknownSchema) {
		t.Errorf("Unwrap error = %v, want ErrUnknownSchema", err)
	}
	decoded := map[string]any{
		"meta": map[string]any{"version": "2"},
		"data": map[string]any{"game": "pokemon"},
	}
	if _, err := UnwrapDocument(decoded); !errors.Is(err, ErrUnknownSchema) {
		t.Errorf("UnwrapDocument error = %v, want ErrUnknownSchema", err)
	}
}

// TestUnwrapAgreesWithItself holds the two spellings to one rule, since a
// bytes reader and a decoded reader that disagreed would read one file two
// ways: both peel an envelope alike and both refuse anything else.
func TestUnwrapAgreesWithItself(t *testing.T) {
	for _, document := range []string{
		`{"meta":{"date":"d","version":"1"},"data":{"game":"pokemon"}}`,
		`{"game":"pokemon","sets":{}}`,
		`{"game":"pokemon","data":{"x":1}}`,
		`{"meta":{"version":"1"},"data":null}`,
	} {
		var whole map[string]any
		if err := json.Unmarshal([]byte(document), &whole); err != nil {
			t.Fatalf("decode %s: %v", document, err)
		}
		peeled, errBytes := Unwrap([]byte(document))
		asDocument, errDocument := UnwrapDocument(whole)
		if !errors.Is(errBytes, ErrNotEnvelope) || !errors.Is(errDocument, ErrNotEnvelope) {
			if errBytes != nil || errDocument != nil {
				t.Errorf("%s: Unwrap error %v, UnwrapDocument error %v", document, errBytes, errDocument)
				continue
			}
			var asBytes map[string]any
			if err := json.Unmarshal(peeled, &asBytes); err != nil {
				t.Fatalf("decode %s: %v", peeled, err)
			}
			if !sameJSON(asBytes, asDocument) {
				t.Errorf("%s: Unwrap = %v, UnwrapDocument = %v", document, asBytes, asDocument)
			}
		}
	}
}

// TestSharedIdentities holds the double-listing rule: a pair of products is
// counted once however many of its printings collide, a handful is
// published, and one past SharedIdentityLimit refuses the build.
func TestSharedIdentities(t *testing.T) {
	var s SharedIdentities
	for _, finish := range []string{"Normal", "Foil", "Normal"} {
		s.Add("product 1", "product 2", "Hizack|GD03-013|"+finish)
	}
	if len(s.pairs) != 1 {
		t.Errorf("one pair colliding in several printings counted %d times", len(s.pairs))
	}
	for i := 2; i <= SharedIdentityLimit; i++ {
		s.Add(fmt.Sprintf("product %d", 10*i), fmt.Sprintf("product %d", 10*i+1), "card")
	}
	if err := s.Check(); err != nil {
		t.Errorf("%d pairs refused: %v", SharedIdentityLimit, err)
	}
	s.Add("product 998", "product 999", "card")
	if err := s.Check(); err == nil {
		t.Errorf("%d pairs published", SharedIdentityLimit+1)
	}
}

// TestCoverageNamesTheLowestOffender pins the zero-skip check every builder
// runs: a product missing or extra is named lowest id first, and an exact
// match passes.
func TestCoverageNamesTheLowestOffender(t *testing.T) {
	want := map[int][]string{1: nil, 2: nil, 3: nil}
	if err := Coverage(map[int][]string{1: nil, 2: nil, 3: nil}, want); err != nil {
		t.Errorf("exact coverage refused: %v", err)
	}
	if err := Coverage(map[int][]string{1: nil}, want); err == nil || !strings.Contains(err.Error(), "2 catalog card products carry no entry, first is 2") {
		t.Errorf("missing products: %v", err)
	}
	if err := Coverage(map[int][]string{1: nil, 2: nil, 3: nil, 9: nil, 7: nil}, want); err == nil || !strings.Contains(err.Error(), "first is 7") {
		t.Errorf("extra products: %v", err)
	}
}

// TestReadCatalogRefusesAnotherGame pins that a dump of the wrong category
// stops a build before anything reads it.
func TestReadCatalogRefusesAnotherGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(`{"category":{"categoryId":91},"groups":[],"products":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCatalog(path, 91); err != nil {
		t.Errorf("right category refused: %v", err)
	}
	if _, err := ReadCatalog(path, 86); err == nil || err.Error() != "category 91, want 86 (wrong game's dump)" {
		t.Errorf("wrong category: %v", err)
	}
}

// TestReadCatalogDropsRepeats pins that a product or group served twice is
// read once, as it was served first, in order, and that products with no id
// are all kept.
func TestReadCatalogDropsRepeats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	data := `{"category":{"categoryId":91},"groups":[{"groupId":1},{"groupId":1}],"products":[
		{"productId":5,"name":"Ace"},{"productId":6,"name":"Two"},{"productId":5,"name":"Ace (Again)"},{"productId":7,"name":"Two"},
		{"name":"Unnumbered"},{"name":"Unnumbered"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalog(path, 91)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, product := range catalog.Products {
		names = append(names, fmt.Sprintf("%d %s", product.ProductID, product.Name))
	}
	want := []string{"5 Ace", "6 Two", "7 Two", "0 Unnumbered", "0 Unnumbered"}
	if !slices.Equal(names, want) || len(catalog.Groups) != 1 {
		t.Errorf("products %q, %d groups; want %q, 1", names, len(catalog.Groups), want)
	}
}

// TestDropRepeatedFinishesKeepsTheFirst pins that a second entry pricing a
// product's finish is dropped, the first kept whatever else it carries, and
// that entries no product prices, or pricing another finish, are kept.
func TestDropRepeatedFinishesKeepsTheFirst(t *testing.T) {
	entry := func(id string, product int, finish string) map[string]any {
		e := map[string]any{"id": id, "finish": finish}
		if product != 0 {
			e["externalLinks"] = map[string]any{"tcgPlayerId": product}
		}
		return e
	}
	kept := DropRepeatedFinishes([]any{
		entry("a", 100, "Normal"), entry("a_foil", 100, "Foil"), entry("b", 100, "Normal"),
		entry("m1", 0, "Normal"), entry("m2", 0, "Normal"), entry("c", 200, "Normal"),
	})
	var ids []string
	for _, e := range kept {
		ids = append(ids, e.(map[string]any)["id"].(string))
	}
	if !slices.Equal(ids, []string{"a", "a_foil", "m1", "m2", "c"}) {
		t.Errorf("kept %v, want b alone dropped", ids)
	}
}

// TestDropEmptySetsKeepsWhatHoldsSomething pins that a set no card and no
// sealed product names is dropped, and one either names is kept.
func TestDropEmptySetsKeepsWhatHoldsSomething(t *testing.T) {
	sets := map[string]any{"AB": map[string]any{}, "BOX": map[string]any{}, "NEXT": map[string]any{}}
	cards := []any{map[string]any{"id": "a", "setCode": "AB"}}
	sealed := []any{map[string]any{"id": "b", "setCode": "BOX"}}
	DropEmptySets(sets, cards, sealed)
	if _, kept := sets["NEXT"]; kept || len(sets) != 2 {
		t.Errorf("sets %v, want AB and BOX alone", sets)
	}
}

// TestSetCodeOfFolds pins that a set code leaves a build folded up and in
// the shape a query can carry. A set code is a case-insensitive token to
// every reader of one: go-mtgban's GetSet, GetUUIDsInSet and
// GetSealedUUIDsInSet each fold the caller's spelling before the lookup, so
// a code that is not already folded is listed everywhere and found nowhere.
//
// TCGplayer abbreviates Gundam's Edition Beta "GD01_b", the one mixed-case
// abbreviation in category 86, and the "GD01-b" once minted from it took its
// set, its 83 cards and the two sealed products sold with them out of reach.
func TestSetCodeOfFolds(t *testing.T) {
	queryable := regexp.MustCompile(`^[A-Z0-9-]+$`)
	for _, test := range []struct{ in, want string }{
		{"GD01_b", "GD01-B"},
		{"GD01", "GD01"},
		{"gd01_b", "GD01-B"},
		{"GCG PR", "GCG-PR"},
		{"OP11 RE", "OP11-RE"},
		{"crz:gg", "CRZ-GG"},
		{"-EVX01-", "EVX01"},
		{"", ""},
	} {
		got := SetCodeOf(test.in)
		if got != test.want {
			t.Errorf("SetCodeOf(%q) = %q, want %q", test.in, got, test.want)
		}
		if got != "" && !queryable.MatchString(got) {
			t.Errorf("SetCodeOf(%q) = %q, which a query cannot carry", test.in, got)
		}
	}
}

// TestSetCodesKeepsTheFirstClaim pins that codes are claimed in group-id
// order, a later group under a taken code is suffixed with its id, and a
// group with no abbreviation is given one from its id.
func TestSetCodesKeepsTheFirstClaim(t *testing.T) {
	codes := SetCodes([]tcgplayer.Group{
		{GroupID: 30, Abbreviation: "GD01"},
		{GroupID: 10, Abbreviation: "gd01"},
		{GroupID: 20, Abbreviation: " "},
	})
	for _, want := range []struct {
		group int
		code  string
	}{
		{10, "GD01"},
		{20, "G20"},
		{30, "GD01-30"},
	} {
		if codes[want.group] != want.code {
			t.Errorf("group %d: %q, want %q", want.group, codes[want.group], want.code)
		}
	}
}

func TestHasDateReadsAMidnightStamp(t *testing.T) {
	for _, test := range []struct {
		publishedOn string
		want        bool
	}{
		{"2024-03-22T00:00:00", true},
		{"2026-10-01T17:14:22.183", false},
	} {
		if got := HasDate(tcgplayer.Group{PublishedOn: test.publishedOn}); got != test.want {
			t.Errorf("HasDate(%q) = %v, want %v", test.publishedOn, got, test.want)
		}
	}
}

func TestQueryNumberKeepsOneToken(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"WTR040 // WTR039", "WTR040//WTR039"},
		{"PW 1", "PW1"},
		{" 128/202 ", "128/202"},
		{"", ""},
	} {
		if got := QueryNumber(test.in); got != test.want {
			t.Errorf("QueryNumber(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestIDBaseSpellsTheNumberIntoTheID(t *testing.T) {
	for _, test := range []struct {
		number  string
		product int
		want    string
	}{
		{"GD01-001", 528, "gd01-001_528"},
		{"BP01/091", 713, "bp01-091_713"},
		{"", 42, "42"},
		{" -/ ", 42, "42"},
	} {
		if got := IDBase(test.number, test.product); got != test.want {
			t.Errorf("IDBase(%q, %d) = %q, want %q", test.number, test.product, got, test.want)
		}
	}
}

func TestLoweredAndMonth(t *testing.T) {
	if got := Lowered([]string{"Store Championship", "SDCC"}); !slices.Equal(got, []string{"store championship", "sdcc"}) {
		t.Errorf("Lowered = %q", got)
	}
	for _, test := range []struct {
		name string
		want int
	}{
		{"september", 9},
		{"September", 9},
		{"Sept", 0},
		{"", 0},
	} {
		if got := Month(test.name); got != test.want {
			t.Errorf("Month(%q) = %d, want %d", test.name, got, test.want)
		}
	}
}

// TestPrintingNamesFollowTheCatalogsOrder pins that a product's printings
// come in the order the catalog displays them, a shared displayOrder broken
// by name, each once however many skus carry it.
func TestPrintingNamesFollowTheCatalogsOrder(t *testing.T) {
	c := &tcgplayer.CatalogDump{
		Printings: []tcgplayer.Printing{
			{PrintingID: 1, Name: "Unlimited Edition Normal", DisplayOrder: 2},
			{PrintingID: 2, Name: "1st Edition Normal", DisplayOrder: 2},
			{PrintingID: 3, Name: "Normal", DisplayOrder: 1},
		},
		Products: []tcgplayer.Product{{ProductID: 7, Skus: []tcgplayer.SKU{
			{PrintingID: 1}, {PrintingID: 2}, {PrintingID: 1}, {PrintingID: 3}, {PrintingID: 99},
		}}},
	}
	want := []string{"Normal", "1st Edition Normal", "Unlimited Edition Normal"}
	if got := PrintingNames(c)[7]; !slices.Equal(got, want) {
		t.Errorf("PrintingNames = %q, want %q", got, want)
	}
}

// TestProductLanguageNamesTheNonEnglishOnes pins that a product with an
// English sku is the English program, and another is named by its
// language, spelled the way the matcher tags it.
func TestProductLanguageNamesTheNonEnglishOnes(t *testing.T) {
	names := LanguageNames(&tcgplayer.CatalogDump{Languages: []tcgplayer.Language{
		{LanguageID: 1, Name: "English"}, {LanguageID: 5, Name: "Chinese (S)"}, {LanguageID: 7, Name: "Japanese"},
	}})
	for _, test := range []struct {
		skus []int
		want string
	}{
		{[]int{1, 7}, ""},
		{[]int{5}, "Chinese Simplified"},
		{[]int{7}, "Japanese"},
		{nil, ""},
	} {
		var skus []tcgplayer.SKU
		for _, id := range test.skus {
			skus = append(skus, tcgplayer.SKU{LanguageID: id})
		}
		if got := ProductLanguage(names, tcgplayer.Product{Skus: skus}); got != test.want {
			t.Errorf("ProductLanguage(%v) = %q, want %q", test.skus, got, test.want)
		}
	}
}
