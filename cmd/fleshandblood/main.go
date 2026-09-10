// Command fleshandblood builds the Flesh and Blood datastore file consumed
// by go-mtgban's mtgmatcher loader, from the TCGplayer catalog dump for
// category 62 and the-fab-cube's community card dataset.
//
// Identity is the catalog's, one entry per product and sku printing: the
// printing names carry both the edition and the treatment axis (Normal,
// Rainbow Foil, Cold Foil and their 1st/Unlimited Edition forms), and
// TCGplayer prices each as its own sku of one product, so each printing is
// its own entry with its own id, priced by construction — the
// finishes-as-flags shape this datastore used to publish folded those
// price points onto one id. The id's finish suffix derives from the
// printing name alone, never from which sibling printings exist, so an id
// cannot churn when TCGplayer later adds a printing to a product.
//
// The name parentheticals follow the One Piece rule, told apart per
// collector number: a parenthetical every product of the number carries is
// part of the card's name (the pitch colors "(Red)"/"(Yellow)"/"(Blue)"),
// a number disambiguator ("(DYN069)") is dropped, and whatever remains is
// the variant label ("Extended Art", "Golden") the matcher narrows on. A
// number with a single product borrows the verdicts the multi-product
// numbers reached, so a lone "(Red)" printing still keeps its pitch in the
// name.
//
// Group abbreviations are unreliable in this category — blank on every
// deck group, reused ("GEM" six times) — and set codes must be unique and
// non-empty, so blanks get a code derived from the group name's initials
// and any code already claimed gets "-groupId" appended, every repair
// logged. Non-blank abbreviations claim their codes first so a derived
// code can never displace a real one.
//
// The-fab-cube dataset knows the game's own printing ids ("MST131") and
// maps 92% of its rows to a TCGplayer product; where exactly one distinct
// id lands on a product the card is annotated with it as fabId. Multiple
// ids landing on one product is expected — treatments share products —
// and annotates nothing. Annotation never changes identity.
//
// The dataset also holds printings whose collector number the catalog has
// no product for at all — the tokens above all, which the game prints and
// TCGplayer does not sell as singles. Those are minted here, so the
// datastore is the sum of both sources rather than the catalog alone: a
// card the game prints is a card that exists, and leaving it out leaves
// every listing of it unresolvable. A minted entry names no product
// because there is none, and the loader groups an entry without a product
// id by its own id with the finish suffix stripped, which is how these are
// built. Its set is the group's where the catalog has one and the
// dataset's own code, name and earliest release date where it does not.
// The finishes are the ones the dataset's edition and foiling name; a pair
// TCGplayer has no printing for — the Gold Cold Foils, the Alpha edition —
// mints no entry of its own, and a card whose every row wears one still
// gets its plain entry so the card exists.
//
// Every product the catalog types as a card becomes an entry, and validate
// refuses a build that left one out: a shape nobody has seen yet stops the
// publish instead of vanishing from the datastore. The products the game
// gives no collector number — the puzzle-art panels, the set and deck art
// cards, the counters, and the handful of playable cards TCGplayer files
// without one — are carried on the id their product alone mints, the same
// shape cmd/pokemon files its basic energies under, and are told apart by
// the set and the variant label the product name spells out.
//
// A product TCGplayer prices only in Japanese skus carries the language it
// is printed in, named from the catalog's own language list. The matcher
// drops a non-English candidate from a query that named no language, so
// the row exists for the listings that name it without English matching
// changing at all.
//
// Sealed products are everything the catalog files outside the singles
// type, by exclusion, so a product type TCGplayer adds later lands on the
// sealed side where it is noticed.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mtgban/datastore-gen/internal/baseline"
	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/go-tcgplayer"
)

const (
	fabCategory = 62

	// englishLanguage is the catalog's language id for English, the one a
	// product needs a sku in to be part of the English program.
	englishLanguage = 1

	fabCardsURL = "https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/card-flattened.json"
	fabSetsURL  = "https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/set.json"
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; everything else is sealed by exclusion.
var tcgSingles = tcgplayer.SinglesProductTypes(fabCategory)

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the distinct printing names its skus
// carry, in the order the catalog displays them; a printing the catalog does not list for a product
// is one that does not exist.

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// fabRow is the slice of a the-fab-cube printing this build reads: the
// game's own printing id, the TCGplayer product the row maps it to, and
// the card's own particulars, which are what a printing the catalog has no
// product for is minted from.
type fabRow struct {
	ID        string `json:"id"`
	SetID     string `json:"set_id"`
	ProductID string `json:"tcgplayer_product_id"`
	Name      string `json:"name"`
	Rarity    string `json:"rarity"`
	Foiling   string `json:"foiling"`
	Edition   string `json:"edition"`
	ImageURL  string `json:"image_url"`
	// Pitch is the value the card pitches for, 1 to 3, which the catalog
	// also carries as "Pitch Value" and gets wrong now and then.
	Pitch string `json:"pitch"`
}

// fabSet is the slice of a the-fab-cube set this build reads, the name and
// date a minted set is filed under.
type fabSet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Printings []struct {
		InitialReleaseDate string `json:"initial_release_date"`
	} `json:"printings"`
}

// releaseDate is the earliest date any printing of the set was released,
// reduced to the bare day the datastore carries.
func (s fabSet) releaseDate() string {
	var earliest string
	for _, printing := range s.Printings {
		date, _, _ := strings.Cut(printing.InitialReleaseDate, "T")
		if date == "" {
			continue
		}
		if earliest == "" || date < earliest {
			earliest = date
		}
	}
	return earliest
}

// fabRarity spells the dataset's one-letter rarity the way the catalog
// spells the same rarity, so a minted card is filed under the vocabulary
// every other card in the datastore uses.
var fabRarity = map[string]string{
	"C": "Common",
	"R": "Rare",
	"S": "Super Rare",
	"M": "Majestic",
	"L": "Legendary",
	"F": "Fabled",
	"T": "Token",
	"B": "Basic",
	"V": "Marvel",
	"P": "Promo",
}

// fabFinish maps a dataset row's edition and foiling to the printing name
// TCGplayer would sell it under, which is the finish vocabulary this
// datastore's ids are suffixed from. The pairs it does not name are the
// ones TCGplayer has no printing for - the Gold Cold Foils, the Alpha
// edition - and a row wearing one is not a price point this scheme can
// spell, so it mints no entry of its own.
var fabFinish = map[string]string{
	"N|S": "Normal",
	"N|R": "Rainbow Foil",
	"N|C": "Cold Foil",
	"F|S": "1st Edition Normal",
	"F|R": "1st Edition Rainbow Foil",
	"F|C": "1st Edition Cold Foil",
	"U|S": "Unlimited Edition Normal",
	"U|R": "Unlimited Edition Rainbow Foil",
}

// idBase mints the id stem an entry's finish suffix hangs off: the
// collector number and the product id, or the product id alone for a
// product the game gives no number.
// numberOf spells a collector number the way a query can carry it. A search
// is split on whitespace before a filter sees it, so the two halves of a
// double-faced number have to stay one token: "WTR040 // WTR039" is
// "WTR040//WTR039", and "PW 1" is "PW1". The separators are already there;
// only the spaces around them go.
func numberOf(number string) string {
	return strings.Join(strings.Fields(number), "")
}

// mintedIDBase is idBase for a printing that names no product: the
// sanitized collector number alone. A catalog id always carries "_<product
// id>" before its finish suffix and a minted one never does, so the two
// namespaces cannot meet.
func mintedIDBase(num string) string {
	num = nonCodeRe.ReplaceAllString(num, "-")
	return strings.ToLower(strings.Trim(num, "-"))
}

func idBase(num string, productID int) string {
	// A double-faced number holds two of them with a separator between,
	// which would put a space inside a uuid.
	num = nonCodeRe.ReplaceAllString(num, "-")
	num = strings.Trim(num, "-")
	if num == "" {
		return strconv.Itoa(productID)
	}
	return strings.ToLower(num) + "_" + strconv.Itoa(productID)
}

// productLanguage names the language a product is printed in, empty for
// the English program: a product TCGplayer prices in no English sku is
// sold in another language, and the catalog's own language list spells out
// which. Several non-English languages on one product would be a shape
// this has never seen, so it is said out loud and the lowest id wins.
func productLanguage(names map[int]string, product tcgplayer.Product) string {
	var ids []int
	for _, sku := range product.Skus {
		if sku.LanguageID == englishLanguage {
			return ""
		}
		if !slices.Contains(ids, sku.LanguageID) {
			ids = append(ids, sku.LanguageID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Ints(ids)
	if len(ids) > 1 {
		log.Printf("%q (%d) prices skus in %d languages, filed under the first",
			product.Name, product.ProductID, len(ids))
	}
	return names[ids[0]]
}

// fetch reads a local path, or an http(s) URL when one is given.
func fetch(location string) ([]byte, error) {
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") {
		return os.ReadFile(location)
	}
	resp, err := http.Get(location)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", location, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// lowered folds a label list to the spelling the matcher declares tags in.
func lowered(quals []string) []string {
	out := make([]string, len(quals))
	for i, q := range quals {
		out[i] = strings.ToLower(q)
	}
	return out
}

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)

// numParenRe matches a collector number worn as a parenthetical
// ("(DYN069)"), including one that disagrees with the Number field, which
// the catalog's typos produce.
var numParenRe = regexp.MustCompile(`^[A-Z]{2,4}\d{3}(?:-[A-Z]{1,2})?$`)

// numTailRe matches the shape a collector number takes when worn as a dash
// suffix, so a tail that looks like a number but disagrees with the Number
// field can be said out loud instead of dropped on a guess.
var numTailRe = regexp.MustCompile(`^[A-Z]{2,6}\s?\d{2,4}(?:-[A-Z]{1,3})?$`)

// componentEq compares one component of a collector number, blind to the
// spacing the catalog sprinkles inside one ("FAB 163" for FAB163).
func componentEq(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), ""), strings.Join(strings.Fields(b), ""))
}

// restatesNumber reports whether a name tail restates the Number field. A
// fused card numbers both faces ("LGS127 // LGS128") while the name wears
// only the face it hangs off, so either side may carry the leading
// component alone.
func restatesNumber(tail, num string) bool {
	if num == "" {
		return false
	}
	tparts := strings.SplitN(tail, "//", 2)
	nparts := strings.SplitN(num, "//", 2)
	if !componentEq(tparts[0], nparts[0]) {
		return false
	}
	if len(tparts) == 2 && len(nparts) == 2 {
		return componentEq(tparts[1], nparts[1])
	}
	return true
}

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []string
	// color is the pitch colour the entry is published with: the one the
	// product's own name says, else the one the dataset gives the printing,
	// else the catalog's Pitch Value, which contradicts the name on
	// twenty-odd products.
	color string
}

// decompose strips the collector number worn as decoration and pulls the
// parenthetical qualifiers out of the name.
func decompose(p tcgplayer.Product, num string) single {
	name := p.Name
	name = strings.ReplaceAll(name, " - "+num, "")

	var quals []string
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if strings.EqualFold(q, num) || numParenRe.MatchString(q) {
			return ""
		}
		// A double-sided name decorates each face ("Ash (Cold Foil) //
		// Aether Ashwing (Cold Foil)"): one qualifier, not two, or the
		// repeat double-votes in the epithet election below.
		if !sliceContains(quals, q) {
			quals = append(quals, q)
		}
		return ""
	})
	// The exact strip above misses the loose decorations ("Gold -  FAB121",
	// "Banneret of Protection - FAB 163"), so what is left of the tail is
	// weighed against the number once more. A number-shaped tail that
	// disagrees with the Number field is an upstream typo on one side or
	// the other, and which side is wrong is not knowable here. Dropping it
	// merges two products whose tails were the only thing telling them
	// apart, so the tail is kept - but as a qualifier rather than in the
	// name, because the variant label is part of the identity a query
	// resolves on and so separates the two just as well, while the name
	// stays the one the card is actually sold under.
	idx := strings.LastIndex(name, " - ")
	if idx >= 0 {
		tail := strings.TrimSpace(name[idx+3:])
		if restatesNumber(tail, num) {
			name = strings.TrimSpace(name[:idx])
		} else if numTailRe.MatchString(tail) {
			name = strings.TrimSpace(name[:idx])
			if !sliceContains(quals, tail) {
				quals = append(quals, tail)
			}
			log.Printf("dash number: %q disagrees with Number %q; kept as a variant", p.Name, num)
		}
	}
	return single{
		product:  p,
		number:   num,
		baseName: strings.Join(strings.Fields(name), " "),
		quals:    quals,
	}
}

// initials reduces a group name to the uppercase initials of its words,
// the deterministic stand-in for an abbreviation the catalog left blank.
func initials(name string) string {
	var b strings.Builder
	for _, word := range strings.FieldsFunc(name, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9')
	}) {
		b.WriteString(strings.ToUpper(word[:1]))
	}
	return b.String()
}

// setCodes assigns every group a unique, non-empty set code. Non-blank
// abbreviations claim their codes first, in group-id order; blank ones get
// the group name's initials; any code already claimed gets "-groupId"
// appended. Every repair is logged, because none of it is the catalog's
// own identity.
// promoGroups reports which catalog groups hand out promotional printings.
// Two things say so and they cover different ground: TCGplayer names the one
// promo group outright, and the welcome decks give their cards away without
// naming themselves promotional, which the products' own rarity records. The
// rarity test asks for every card product in the group, not most: a set that
// merely holds some promos among its pack cards is not a promotional set,
// and reading it as one would make a promo of everything beside them.
func promoGroups(catalog tcgplayer.CatalogDump) map[int]bool {
	cards := map[int]int{}
	promos := map[int]int{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		cards[product.GroupID]++
		if strings.EqualFold(product.Extended("Rarity"), promoRarity) {
			promos[product.GroupID]++
		}
	}
	out := map[int]bool{}
	for _, group := range catalog.Groups {
		if strings.Contains(strings.ToLower(group.Name), "promo") {
			out[group.GroupID] = true
			continue
		}
		if n := cards[group.GroupID]; n > 0 && promos[group.GroupID] == n {
			out[group.GroupID] = true
		}
	}
	return out
}

// promoRarity is what the catalog calls the rarity of a printing handed out
// rather than sold in a pack.
const promoRarity = "Promo"

// nonCodeRe matches the runs a set code cannot carry.
var nonCodeRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// normalizeSetName reduces a set name to what the two sources spell alike:
// the dataset writes "Armory Deck - Azalea" where the catalog writes
// "Armory Deck: Azalea", and only the punctuation between them differs.
func normalizeSetName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// setCodeOf reduces a catalog abbreviation to what a search query can carry.
// A set code is typed after "is:", and a query is split on whitespace before
// a filter ever sees it and on the colon that names the filter, so a code
// holding either cannot be asked for: "is:OP11 RE" reaches the filter as
// "is:OP11" and "is:crz:gg" names a filter called crz. Every run of anything
// but a letter or a digit becomes one dash, and the ends are trimmed of them.
func setCodeOf(abbreviation string) string {
	return strings.Trim(nonCodeRe.ReplaceAllString(abbreviation, "-"), "-")
}

func setCodes(groups []tcgplayer.Group) map[int]string {
	ordered := append([]tcgplayer.Group(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].GroupID < ordered[j].GroupID
	})

	codes := map[int]string{}
	taken := map[string]bool{}
	claim := func(g tcgplayer.Group, code string) {
		if taken[code] {
			suffixed := fmt.Sprintf("%s-%d", code, g.GroupID)
			log.Printf("set code: %q (%s) reuses %s, using %s", g.Name, g.Abbreviation, code, suffixed)
			code = suffixed
		}
		codes[g.GroupID] = code
		taken[code] = true
	}
	for _, g := range ordered {
		if code := setCodeOf(g.Abbreviation); code != "" {
			claim(g, code)
		}
	}
	for _, g := range ordered {
		if setCodeOf(g.Abbreviation) != "" {
			continue
		}
		code := initials(g.Name)
		if code == "" {
			code = fmt.Sprintf("G%d", g.GroupID)
		}
		log.Printf("set code: %q has no abbreviation, derived %s", g.Name, code)
		claim(g, code)
	}
	return codes
}

func printingNames(c *tcgplayer.CatalogDump) map[int][]string {
	name := map[int]string{}
	for _, p := range c.Printings {
		name[p.PrintingID] = p.Name
	}

	// The order TCGplayer displays a category's printings in, which is the
	// catalog's to decide: a list written here would be a second opinion
	// about somebody else's data. Two printings can share a displayOrder -
	// Flesh and Blood has three at 2 - so the name settles a tie and the
	// order stays fixed for unchanged data.
	rank := map[string]int{}
	for _, p := range c.Printings {
		rank[p.Name] = p.DisplayOrder
	}

	out := map[int][]string{}
	for _, product := range c.Products {
		var names []string
		for _, sku := range product.Skus {
			n := name[sku.PrintingID]
			if n == "" || sliceContains(names, n) {
				continue
			}
			names = append(names, n)
		}
		sort.SliceStable(names, func(i, j int) bool {
			if ri, rj := rank[names[i]], rank[names[j]]; ri != rj {
				return ri < rj
			}
			return names[i] < names[j]
		})
		out[product.ProductID] = names
	}
	return out
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 62 (required)")
	fabCards := flag.String("fab-cards", fabCardsURL, "the-fab-cube card-flattened file, path or URL")
	fabSets := flag.String("fab-sets", fabSetsURL, "the-fab-cube set file, path or URL")
	against := flag.String("against", "", "baseline datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.01, "the share of its cards or sealed products a build may lose")
	baselineFit := flag.String("baseline-fit", "", "write this file when the build is fit to become the baseline the next build compares against")
	flag.Parse()

	if *catalogPath == "" {
		log.Fatalln("-tcg-catalog is required: the dump carries the printings and the ids")
	}
	catalogData, err := os.ReadFile(*catalogPath)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	var catalog tcgplayer.CatalogDump
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != fabCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, fabCategory)
	}

	fabData, err := fetch(*fabCards)
	if err != nil {
		log.Fatalln("fab dataset:", err)
	}
	setsData, err := fetch(*fabSets)
	if err != nil {
		log.Fatalln("fab sets:", err)
	}
	var fabSetRows []fabSet
	if err := json.Unmarshal(setsData, &fabSetRows); err != nil {
		log.Fatalln("fab sets:", err)
	}
	fabSetByID := map[string]fabSet{}
	for _, set := range fabSetRows {
		fabSetByID[strings.ToUpper(set.ID)] = set
	}
	var fabRows []fabRow
	if err := json.Unmarshal(fabData, &fabRows); err != nil {
		log.Fatalln("fab dataset:", err)
	}
	log.Printf("catalog: %d groups, %d products; fab dataset: %d printings",
		len(catalog.Groups), len(catalog.Products), len(fabRows))

	groupByID := map[int]tcgplayer.Group{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	codes := setCodes(catalog.Groups)
	checkFabFinishNames(&catalog)
	printings := printingNames(&catalog)
	displayOrder := printingDisplayOrder(&catalog)

	// Split the products: every single becomes printings, the non-single
	// types become sealed.
	var singles []single
	var sealedProducts []tcgplayer.Product
	var unnumbered int
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			// Every card product the catalog has ever carried prices at
			// least one sku, and a product with none has no printing to
			// file an entry under: stop rather than drop it.
			log.Fatalf("no sku printing: %q (%d) has no entry to carry it",
				product.Name, product.ProductID)
		}
		num := product.Extended("Number")
		if num == "" {
			// Art cards, counters, uncut-sheet pieces: the product id is
			// the whole id, as it is for the numberless Pokemon singles.
			unnumbered++
		}
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept (%d unnumbered)", len(singles), unnumbered)

	// Per collector number: a qualifier every product of the number
	// carries is part of the name (the pitch colors), not a variant. A
	// number with a single product cannot make that call alone, so the
	// epithets learned from the multi-product numbers decide for it.
	byNumber := map[string][]*single{}
	for i := range singles {
		// The unnumbered products are unrelated cards, not one card's
		// printings, so they elect nothing together: they take the
		// verdicts the real numbers reached, as a lone printing does.
		if singles[i].number == "" {
			continue
		}
		byNumber[singles[i].number] = append(byNumber[singles[i].number], &singles[i])
	}
	nameParens := map[string]bool{}
	for _, bucket := range byNumber {
		sort.Slice(bucket, func(i, j int) bool {
			return bucket[i].product.ProductID < bucket[j].product.ProductID
		})
		if len(bucket) < 2 {
			continue
		}
		common := map[string]int{}
		for _, s := range bucket {
			for _, q := range s.quals {
				common[q]++
			}
		}
		for q, n := range common {
			if n == len(bucket) {
				nameParens[q] = true
			}
		}
	}
	var learned []string
	for q := range nameParens {
		learned = append(learned, q)
	}
	sort.Strings(learned)
	log.Printf("name parentheticals learned: %v", learned)
	assemble := func(s *single, isName map[string]bool) {
		var name, variant []string
		name = append(name, s.baseName)
		for _, q := range s.quals {
			if isName[q] {
				name = append(name, "("+q+")")
			} else {
				variant = append(variant, q)
			}
		}
		s.baseName = strings.Join(name, " ")
		s.quals = variant
	}
	for _, bucket := range byNumber {
		// Decide before mutating: the membership test must read every
		// product's original qualifiers, not the ones a fold already moved.
		isName := map[string]bool{}
		if len(bucket) < 2 {
			for _, q := range bucket[0].quals {
				isName[q] = nameParens[q]
			}
		} else {
			common := map[string]int{}
			for _, s := range bucket {
				for _, q := range s.quals {
					common[q]++
				}
			}
			for q, n := range common {
				isName[q] = n == len(bucket)
			}
		}
		for _, s := range bucket {
			assemble(s, isName)
		}
	}
	for i := range singles {
		if singles[i].number != "" {
			continue
		}
		assemble(&singles[i], nameParens)
	}

	// Annotate the game's own printing id where the dataset maps exactly
	// one distinct id to the product. Several ids on one product is the
	// treatments sharing it, and picking one would be a guess; a product
	// the dataset does not know is a coverage gap, expected on promos.
	// Both are counted, neither changes identity.
	catalogProducts := map[int]bool{}
	for _, product := range catalog.Products {
		catalogProducts[product.ProductID] = true
	}
	cardProducts := map[int]bool{}
	groupOfCard := map[int]int{}
	for _, s := range singles {
		cardProducts[s.product.ProductID] = true
		groupOfCard[s.product.ProductID] = s.product.GroupID
	}
	fabIDsByProduct := map[int][]string{}
	// The pitch the dataset gives each product's printings, kept where
	// every printing of the product agrees.
	pitchByProduct := map[int]map[string]bool{}
	// The card product a dataset number is sold as, by the product id the
	// dataset writes on its rows. It is what decides below whether a number
	// is carried: the catalog spells a number its own way often enough -
	// RNR001 for the deck the dataset numbers RHI001, one face of a fused
	// token for both - that the number alone said "no product" for 202
	// numbers TCGplayer sells, and each was minted a second time beside
	// its priced entry.
	productByNumber := map[string]int{}
	// The catalog group a dataset set's rows are sold under, counted per
	// group, so a set the two sources code differently can be joined by
	// where its cards actually are rather than by what it is called.
	groupsByDatasetSet := map[string]map[int]int{}
	var unknownIDs int
	for _, row := range fabRows {
		if row.ProductID == "" {
			continue
		}
		var productID int
		if _, err := fmt.Sscanf(row.ProductID, "%d", &productID); err != nil {
			log.Fatalf("fab dataset: printing %s carries product id %q", row.ID, row.ProductID)
		}
		if !catalogProducts[productID] {
			unknownIDs++
			continue
		}
		if !sliceContains(fabIDsByProduct[productID], row.ID) {
			fabIDsByProduct[productID] = append(fabIDsByProduct[productID], row.ID)
		}
		if color := pitchColors[strings.TrimSpace(row.Pitch)]; color != "" {
			if pitchByProduct[productID] == nil {
				pitchByProduct[productID] = map[string]bool{}
			}
			pitchByProduct[productID][color] = true
		}
		if !cardProducts[productID] {
			continue
		}
		if number := strings.ToUpper(numberOf(row.ID)); number != "" {
			if _, held := productByNumber[number]; !held {
				productByNumber[number] = productID
			}
		}
		if setID := strings.ToUpper(setCodeOf(row.SetID)); setID != "" {
			if groupsByDatasetSet[setID] == nil {
				groupsByDatasetSet[setID] = map[int]int{}
			}
			groupsByDatasetSet[setID][groupOfCard[productID]]++
		}
	}
	fabIDs := map[int]string{}
	var multiMapped, uncovered int
	uncoveredBySet := map[string]int{}
	for _, s := range singles {
		ids := fabIDsByProduct[s.product.ProductID]
		switch len(ids) {
		case 0:
			uncovered++
			uncoveredBySet[codes[s.product.GroupID]]++
		case 1:
			fabIDs[s.product.ProductID] = ids[0]
		default:
			multiMapped++
		}
	}
	log.Printf("fab ids: %d of %d printings annotated, %d products multi-mapped (none picked), %d uncovered",
		len(fabIDs), len(singles), multiMapped, uncovered)
	if unknownIDs > 0 {
		log.Printf("fab ids: %d dataset rows point at products the catalog does not carry", unknownIDs)
	}
	if uncovered > 0 {
		log.Printf("uncovered printings by set: %v", uncoveredBySet)
	}

	// A product the catalog files with no collector number, whose every
	// dataset row is one printing, is that printing: it takes the number
	// the dataset gives it, which is the fabId it already carried. Left
	// unnumbered it could not be asked for by number at all.
	var numbered int
	for i := range singles {
		s := &singles[i]
		if s.number != "" {
			continue
		}
		if id, found := fabIDs[s.product.ProductID]; found {
			s.number = id
			numbered++
			log.Printf("number: %q (%d) carries no collector number; the dataset files its one printing at %s", s.product.Name, s.product.ProductID, id)
		}
	}
	if numbered > 0 {
		log.Printf("number: %d unnumbered products took their number from the dataset", numbered)
	}

	// The pitch colour, from the three places it is written. The name is
	// first: "(Red)" on the product is the colour the card is sold as, and
	// the catalog's Pitch Value field contradicts it on twenty-odd
	// products - "Life of the Party (Red)" with a Pitch Value of 2. The
	// dataset's pitch is next, for the products the catalog names without
	// a colour, and the field is last, for the printings the dataset does
	// not map. The field's disagreements are counted rather than trusted.
	var fieldDisagrees int
	for i := range singles {
		s := &singles[i]
		field := pitchColor(s.product)
		s.color = field
		if match := pitchInName.FindStringSubmatch(s.baseName); match != nil {
			s.color = match[1]
		} else if pitches := pitchByProduct[s.product.ProductID]; len(pitches) == 1 {
			for color := range pitches {
				s.color = color
			}
		}
		if field != "" && s.color != field {
			fieldDisagrees++
		}
	}
	if fieldDisagrees > 0 {
		log.Printf("color: the catalog's Pitch Value disagrees with the name or the dataset on %d products; the name and the dataset win", fieldDisagrees)
	}

	// The other direction, which nothing counted before: a dataset row
	// whose collector number no card product carries is a card the catalog
	// does not sell. The counts above measure only how much of the catalog
	// the dataset could annotate, so a card the game prints and TCGplayer
	// does not sell as a single - the tokens above all - was invisible,
	// annotating nothing and showing up as no gap. These are what the
	// minting below adds, so the datastore holds both sources rather than
	// the catalog alone.
	//
	// A number is carried when the catalog sells it, and the catalog says
	// so three ways. It spells the number itself on a product. It spells
	// both faces of a fused token on one product, "WTR040 // WTR113",
	// where the dataset numbers each face alone. And it sells the card
	// under a number of its own - RNR001 for the deck the dataset numbers
	// RHI001, FAB384 for the product whose name says FAB385 - which only
	// the product id the dataset writes on the row can tell. Reading the
	// number alone minted 250 twins of priced products, unpriced, filed in
	// 22 sets that were mostly the catalog's own groups under another code.
	catalogNumbers := map[string]bool{}
	faceNumbers := map[string]bool{}
	for _, s := range singles {
		if s.number == "" {
			continue
		}
		number := strings.ToUpper(numberOf(s.number))
		catalogNumbers[number] = true
		if faces := strings.Split(number, "//"); len(faces) > 1 {
			for _, face := range faces {
				faceNumbers[face] = true
			}
		}
	}
	// The catalog group a dataset set sells under: the one most of its
	// mapped rows' products sit in. A majority of the rows, so a set split
	// across groups - which nothing here is today - names none rather than
	// the larger half. It decides which set a minted card is filed under,
	// below, and it is what a numberless product can be read against here.
	groupBySales := map[string]int{}
	for setID, groups := range groupsByDatasetSet {
		var total, best, bestGroup int
		for groupID, n := range groups {
			total += n
			if n > best || (n == best && groupID < bestGroup) {
				best, bestGroup = n, groupID
			}
		}
		if best*2 > total {
			groupBySales[setID] = bestGroup
		}
	}
	// The names of the card products the catalog files with no number,
	// per group. A dataset row the catalog maps to nothing, whose set sells
	// under a group holding a numberless product of the same name, is that
	// product on every fact either source offers - the promo shelf's "Fault
	// Line" beside the dataset's FAB328 - and read as a new card it was
	// minted beside it, and a listing naming the card without a number
	// aliased between the two.
	numberlessNames := map[int]map[string]bool{}
	for _, s := range singles {
		if s.number != "" {
			continue
		}
		if numberlessNames[s.product.GroupID] == nil {
			numberlessNames[s.product.GroupID] = map[string]bool{}
		}
		numberlessNames[s.product.GroupID][strings.ToLower(s.baseName)] = true
	}
	mintable := map[string][]fabRow{}
	var mintableOrder []string
	var coveredByFace, coveredByProduct, coveredByName int
	for _, row := range fabRows {
		number := strings.ToUpper(numberOf(row.ID))
		if number == "" || catalogNumbers[number] {
			continue
		}
		if faceNumbers[number] {
			coveredByFace++
			continue
		}
		if _, sold := productByNumber[number]; sold {
			coveredByProduct++
			continue
		}
		if _, seen := mintable[number]; !seen {
			mintableOrder = append(mintableOrder, number)
		}
		mintable[number] = append(mintable[number], row)
	}
	// Only where the name is unambiguous in that group: two unmapped rows
	// of one name cannot both be the one numberless product, and covering
	// both would drop a card the game does print.
	sameName := map[string][]string{}
	for _, number := range mintableOrder {
		row := mintable[number][0]
		group, sold := groupBySales[strings.ToUpper(setCodeOf(row.SetID))]
		if !sold || !numberlessNames[group][strings.ToLower(row.Name)] {
			continue
		}
		key := fmt.Sprintf("%d|%s", group, strings.ToLower(row.Name))
		sameName[key] = append(sameName[key], number)
	}
	for _, numbers := range sameName {
		if len(numbers) != 1 {
			continue
		}
		number := numbers[0]
		log.Printf("dataset printing %s is the numberless %q the catalog sells in the set its rows are sold under; not minted",
			mintable[number][0].ID, mintable[number][0].Name)
		coveredByName++
		delete(mintable, number)
	}
	mintableOrder = slices.DeleteFunc(mintableOrder, func(number string) bool {
		_, kept := mintable[number]
		return !kept
	})
	sort.Strings(mintableOrder)
	var mintableRows int
	mintableBySet := map[string]int{}
	for _, number := range mintableOrder {
		mintableRows += len(mintable[number])
		mintableBySet[mintable[number][0].SetID]++
	}
	log.Printf("dataset printings the catalog has no product for: %d rows over %d collector numbers in %d sets",
		mintableRows, len(mintableOrder), len(mintableBySet))
	log.Printf("dataset printings the catalog sells under a number of its own: %d rows by the product they map to, %d as one face of a fused product, %d by name beside a numberless product",
		coveredByProduct, coveredByFace, coveredByName)

	// Emit. Sets are the catalog groups under their repaired codes; ids
	// embed the product id so they survive any upstream renumbering.
	sets := map[string]any{}
	promoted := promoGroups(catalog)
	var promoSets int
	// An empty group is skipped, its code left claimed so nothing renames
	// while it is empty and the set appears already-coded the day
	// TCGplayer files a product there.
	productsIn := map[int]int{}
	for _, product := range catalog.Products {
		productsIn[product.GroupID]++
	}
	var skippedEmpty int
	for _, group := range catalog.Groups {
		if productsIn[group.GroupID] == 0 {
			skippedEmpty++
			continue
		}
		set := map[string]any{
			"name":        group.Name,
			"releaseDate": group.ReleaseDate(),
		}
		// The type is what tells the matcher a printing is promotional, so
		// only the wholly promotional groups carry it.
		if promoted[group.GroupID] {
			set["type"] = "promo"
			promoSets++
		}
		sets[codes[group.GroupID]] = set
	}
	if skippedEmpty > 0 {
		log.Printf("sets: %d empty groups hold no product and are skipped", skippedEmpty)
	}
	log.Printf("promotional sets: %d of %d", promoSets, len(sets))

	// The sets a minted card is filed under. A dataset set the catalog has
	// a group for is that group's set, under the code the group already
	// claimed, so a minted card lands beside the printings TCGplayer does
	// sell. A set the catalog has no group for at all is minted from the
	// dataset's own code, name and earliest release date, deduplicated
	// against the codes the catalog groups already hold so nothing can
	// fold onto them.
	//
	// The catalog group a dataset set belongs to, found three ways. Where
	// the set's rows are sold is the first and the surest: the product ids
	// the dataset writes on its rows land in a catalog group, and where
	// most of them land in one group that group is the set - the dataset's
	// promo set FAB sells 399 of 400 mapped rows under "Flesh and Blood:
	// Promo Cards", and each Silver Age hero's set sells under its chapter.
	// A set nothing prices has no rows to follow, and falls through. Its
	// abbreviation is next and the one that fails most often: the dataset
	// codes a set "AAZ" where TCGplayer abbreviates the same set "ADA", or
	// does not abbreviate it at all and takes a code derived from its name.
	// The name is what the two sources really agree on - "Armory Deck -
	// Azalea" against "Armory Deck: Azalea", differing by the punctuation
	// the normalization drops - so it is tried last, and all three are what
	// keep a minted card in the set holding the printings TCGplayer does
	// sell rather than in a second set of the same name.
	codeBySales := map[string]string{}
	for setID, groupID := range groupBySales {
		if productsIn[groupID] > 0 {
			codeBySales[setID] = codes[groupID]
		}
	}
	codeByAbbreviation := map[string]string{}
	codeByName := map[string]string{}
	for _, group := range catalog.Groups {
		// A minted card must land in a set that exists, and an empty
		// group's set was skipped above: its abbreviation falls through
		// to the minted-set path instead of naming a set nothing emitted.
		if productsIn[group.GroupID] == 0 {
			continue
		}
		if name := normalizeSetName(group.Name); name != "" {
			if _, taken := codeByName[name]; !taken {
				codeByName[name] = codes[group.GroupID]
			}
		}
		abbreviation := strings.ToUpper(setCodeOf(group.Abbreviation))
		if abbreviation == "" {
			continue
		}
		if _, taken := codeByAbbreviation[abbreviation]; !taken {
			codeByAbbreviation[abbreviation] = codes[group.GroupID]
		}
	}
	takenCodes := map[string]bool{}
	for _, code := range codes {
		takenCodes[code] = true
	}
	mintedSetCode := map[string]string{}
	var mintedSets int
	for _, number := range mintableOrder {
		setID := strings.ToUpper(setCodeOf(mintable[number][0].SetID))
		if setID == "" {
			log.Fatalf("dataset row %q names no set", mintable[number][0].ID)
		}
		if _, decided := mintedSetCode[setID]; decided {
			continue
		}
		if code, found := codeBySales[setID]; found {
			mintedSetCode[setID] = code
			if code != codeByAbbreviation[setID] {
				log.Printf("dataset set %s joins catalog set %s, where its rows are sold", setID, code)
			}
			continue
		}
		if code, found := codeByAbbreviation[setID]; found {
			mintedSetCode[setID] = code
			continue
		}
		if code, found := codeByName[normalizeSetName(fabSetByID[setID].Name)]; found {
			mintedSetCode[setID] = code
			log.Printf("dataset set %s joins catalog set %s by name (%q)",
				setID, code, fabSetByID[setID].Name)
			continue
		}
		code := setID
		if takenCodes[code] {
			code = code + "-fab"
			log.Printf("dataset set %s: code already taken, minted set code %s", setID, code)
		}
		if takenCodes[code] {
			log.Fatalf("minted set code %s still not unique; refusing to guess further", code)
		}
		takenCodes[code] = true
		mintedSetCode[setID] = code
		upstream := fabSetByID[setID]
		name := upstream.Name
		if name == "" {
			name = setID
		}
		set := map[string]any{
			"name":        name,
			"releaseDate": upstream.releaseDate(),
		}
		sets[code] = set
		mintedSets++
	}
	if mintedSets > 0 {
		log.Printf("sets minted for dataset sets the catalog has no group for: %d", mintedSets)
	}

	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	// The coverage contract: every product the catalog types as a card,
	// with the sku printings it is sold in. validate reads it back off the
	// encoded output, so a product no rule here carried fails the build
	// instead of quietly leaving the datastore.
	catalogFinishes := map[int][]string{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		catalogFinishes[product.ProductID] = printings[product.ProductID]
	}

	languageNames := map[int]string{}
	for _, language := range catalog.Languages {
		languageNames[language.LanguageID] = language.Name
	}

	var cards []any
	var nonEnglish int
	for _, s := range singles {
		productID := s.product.ProductID
		language := productLanguage(languageNames, s.product)
		if language != "" {
			nonEnglish++
		}
		for _, finish := range printings[productID] {
			suffix := emit.FinishSuffix(finish)
			entry := map[string]any{
				"id":      idBase(s.number, productID) + suffix,
				"name":    s.baseName,
				"setCode": codes[s.product.GroupID],
				"rarity":  s.product.Extended("Rarity"),
				"finish":  finish,
				"image":   emit.ImageURL(s.product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if s.number != "" {
				entry["number"] = numberOf(s.number)
			}
			if s.color != "" {
				entry["color"] = s.color
			}
			if language != "" {
				entry["language"] = language
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
				// The same labels as a list, because joining them loses
				// where one ends and the next begins: "Cold Foil Extended
				// Art" cannot be read back into its two tags, and the
				// matcher needs them whole to declare and to match on.
				entry["promoTypes"] = lowered(s.quals)
			}
			if id, found := fabIDs[productID]; found {
				entry["fabId"] = id
			}
			cards = append(cards, entry)
		}
	}

	// Mint the printings the catalog has no product for. The game prints
	// them and TCGplayer does not sell them as singles - the tokens above
	// all - and a datastore leaving them out leaves every listing of one
	// unresolvable, so the datastore carries the sum of both sources
	// rather than the catalog alone. A minted entry names no product,
	// because there is none: nothing prices it, and the loader groups an
	// entry without a product id by its own id with the finish suffix
	// stripped, which is exactly how these are built. The finishes are the
	// ones the dataset's edition and foiling name, and a card whose every
	// row wears a pair TCGplayer has no printing for still gets its plain
	// entry, so the card exists even where its treatments cannot be spelled.
	var mintedCards, unspellable int
	for _, number := range mintableOrder {
		rows := mintable[number]
		row := rows[0]
		code := mintedSetCode[strings.ToUpper(setCodeOf(row.SetID))]
		rarity := fabRarity[row.Rarity]
		if rarity == "" && row.Rarity != "" {
			log.Printf("dataset rarity %q on %s is not one this datastore spells", row.Rarity, row.ID)
		}

		var finishes []string
		for _, r := range rows {
			finish, known := fabFinish[r.Edition+"|"+r.Foiling]
			if !known {
				unspellable++
				continue
			}
			if !sliceContains(finishes, finish) {
				finishes = append(finishes, finish)
			}
		}
		if len(finishes) == 0 {
			finishes = []string{emit.PlainPrinting(&catalog)}
		}
		// The same display order the emitted entries are ranked by.
		sort.SliceStable(finishes, func(i, j int) bool {
			if ri, rj := displayOrder[finishes[i]], displayOrder[finishes[j]]; ri != rj {
				return ri < rj
			}
			return finishes[i] < finishes[j]
		})

		for _, finish := range finishes {
			entry := map[string]any{
				"id":      mintedIDBase(number) + emit.FinishSuffix(finish),
				"name":    row.Name,
				"number":  numberOf(row.ID),
				"setCode": code,
				"rarity":  rarity,
				"finish":  finish,
				"image":   row.ImageURL,
				"fabId":   row.ID,
			}
			cards = append(cards, entry)
			mintedCards++
		}
	}
	if mintedCards > 0 {
		log.Printf("minted: %d entries over %d collector numbers the catalog has no product for (%d rows wear a finish this scheme cannot spell)",
			mintedCards, len(mintableOrder), unspellable)
	}

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []any
	for _, product := range sealedProducts {
		group := groupByID[product.GroupID]
		sealed = append(sealed, map[string]any{
			"id":          fmt.Sprintf("%s-%d", strings.ToLower(codes[group.GroupID]), product.ProductID),
			"name":        product.Name,
			"setCode":     codes[group.GroupID],
			"releaseDate": group.ReleaseDate(),
			"image":       emit.ImageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	dropped, tokens := foldPromoTypes(cards)
	log.Printf("promo types: %d labels dropped as the printing's own facts, %d tokens left, each one a slug", dropped, tokens)
	log.Printf("emitting %d sets, %d card entries over %d products (%d not in English), %d sealed",
		len(sets), len(cards), len(singles), nonEnglish, len(sealed))
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	// The id upstream knows this printing by, in the place every other
	// identifier lives. It has been written flat on the entry beside an
	// externalLinks holding only the TCGplayer id, so "which id spaces is
	// this card in" has been two questions rather than one - and three
	// across the eight games, because Riftbound writes the TCGplayer id
	// flat as well. It is written in both places for now: the loader reads
	// the flat one, and the flat one goes when it reads this one instead.
	var linked int
	for _, entry := range cards {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["fabId"].(string)
		if id == "" {
			continue
		}
		links, ok := item["externalLinks"].(map[string]any)
		if !ok {
			links = map[string]any{}
			item["externalLinks"] = links
		}
		links["fabId"] = id
		linked++
	}
	log.Printf("external links: %d cards carry their fabId under externalLinks as well", linked)

	doc := map[string]any{
		"game":   "fleshandblood",
		"sets":   sets,
		"cards":  cards,
		"sealed": sealed,
	}
	var buf bytes.Buffer
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	emit.PlainQuotes(doc)

	if err := json.NewEncoder(&buf).Encode(doc); err != nil {
		log.Fatalln(err)
	}

	// Re-read the encoded output and verify it structurally before
	// publishing anything: a format drift or a truncated download must
	// fail here, not in every consumer. The types mirror what go-mtgban's
	// loader reads, duplicated so this repository depends on nothing.
	counted, err := validate(buf.Bytes(), catalogFinishes)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d sealed", counted.sets, counted.cards, counted.sealed)
	if counted.cards != len(cards) || counted.sealed != len(sealed) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			len(cards), len(sealed), counted.cards, counted.sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a single, so it is exhaustive by
	// construction and cannot lose a product to a rule that did not know
	// what to do with it - the card side's whole failure mode. What it can
	// lose a product to is an edit: one `continue` on the sealed path and
	// the products would leave the datastore with nothing to say so, the
	// card side's invariant being blind to them. Counting the emitted
	// products back against the catalog total is what says so.
	wantSealed := len(catalog.Products) - len(singles)
	if counted.sealed != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			counted.sealed, wantSealed)
	}

	// Compare against the baseline, when the publish handed one over, and
	// say whether this build is fit to become the next one.
	if err := baseline.Guard(buf.Bytes(), baseline.Count, baseline.Options{
		Against: *against, Tolerance: *againstTolerance, FitPath: *baselineFit,
	}); err != nil {
		log.Fatalln(err)
	}

	out := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			log.Fatalln(err)
		}
		defer f.Close()
		out = f
	}
	if _, err := out.Write(buf.Bytes()); err != nil {
		log.Fatalln(err)
	}
}

type counts struct {
	sets, cards, sealed int
}

// coverage is the zero-skip invariant: the products the emitted entries
// cover must be exactly the products the catalog types as cards. Checked on
// the encoded output, so a card product no rule above knew what to do with
// stops the publish instead of quietly leaving the datastore. The offender
// is named lowest id first, so the same data always reports the same one.
func coverage(got, want map[int][]string) error {
	var missing, extra []int
	for productID := range want {
		_, found := got[productID]
		if !found {
			missing = append(missing, productID)
		}
	}
	for productID := range got {
		_, found := want[productID]
		if !found {
			extra = append(extra, productID)
		}
	}
	sort.Ints(missing)
	sort.Ints(extra)
	if len(missing) > 0 {
		return fmt.Errorf("%d catalog card products carry no entry, first is %d",
			len(missing), missing[0])
	}
	if len(extra) > 0 {
		return fmt.Errorf("%d entries name a product the catalog does not type as a card, first is %d",
			len(extra), extra[0])
	}
	return nil
}

// validate decodes an encoded datastore and checks its shape: every card
// and sealed product carrying its identity, every id unique within its
// namespace, every referenced set existing, every finish one of the eight
// printing names, and every product's entries covering exactly the sku
// printings the catalog lists for it.
// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// idShape is what a uuid has to look like wherever one is written down: a
// slash is a path separator and a space ends a word, and a uuid travels
// through urls, filenames and query strings alike.
var idShape = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validate(data []byte, wantFinishes map[int][]string) (counts, error) {
	var doc struct {
		Game string `json:"game"`
		Sets map[string]struct {
			Name string `json:"name"`
		} `json:"sets"`
		Cards []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Number        string `json:"number"`
			SetCode       string `json:"setCode"`
			Variant       string `json:"variant"`
			Language      string `json:"language"`
			Finish        string `json:"finish"`
			FabID         string `json:"fabId"`
			ExternalLinks struct {
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"cards"`
		Sealed []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"sealed"`
	}
	var out counts
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}

	if doc.Game != "fleshandblood" {
		return out, fmt.Errorf("game is %q, not fleshandblood", doc.Game)
	}
	for code, set := range doc.Sets {
		if set.Name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cardIDs := map[string]bool{}
	// A query resolves a card by its name, number, set and variant label,
	// and folds a product's finishes onto the product id before it picks
	// one, so two products wearing all four alike are one card to every
	// consumer and would alias each other's prices. The key holds the
	// product id rather than a flag so a product's own eight printings pass
	// while two different products never do — keying on the finish instead
	// would wave through exactly the pair this is meant to catch, since the
	// promos that collide carry a single printing each.
	// The discriminator two entries wearing one identity are told apart by:
	// the product for an entry that names one, and the card key for a
	// minted entry, which names no product because none exists. A minted
	// card's own finishes share that key and pass, exactly as a product's
	// sibling printings do.
	identities := map[string]string{}
	gotFinishes := map[int][]string{}
	// The loader groups a minted entry with its siblings by the fabId it
	// was minted from, so a minted entry wearing a fabId a priced entry
	// also wears is that card a second time, unpriced, and every listing of
	// it can land on either. Which fabIds are priced is read off the
	// document first, since a product's entries may sort after a minted
	// one's.
	pricedFabIDs := map[string]int{}
	for _, card := range doc.Cards {
		if card.FabID != "" && card.ExternalLinks.TcgPlayerID != 0 {
			pricedFabIDs[card.FabID] = card.ExternalLinks.TcgPlayerID
		}
	}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" || card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q", card.Name, card.ID, card.Number)
		}
		if card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) carries no finish at all", card.Name, card.ID)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		// The language is part of the identity for the same reason the
		// variant label is: the matcher narrows on it, so the Japanese
		// printing of a card is not the English one wearing its name.
		identity := strings.Join([]string{
			card.Name, card.Number, card.SetCode, card.Variant, card.Language}, "|")
		productID := card.ExternalLinks.TcgPlayerID
		discriminator := fmt.Sprint(productID)
		if productID == 0 {
			discriminator = "minted:" + card.SetCode + "|" + card.Number
			if priced, sold := pricedFabIDs[card.FabID]; sold {
				return out, fmt.Errorf("minted %s is %s a second time: product %d already sells it",
					card.ID, card.FabID, priced)
			}
		}
		other, seen := identities[identity]
		if seen && other != discriminator {
			return out, fmt.Errorf("%s and %s wear one identity: %s",
				other, discriminator, identity)
		}
		identities[identity] = discriminator
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		// A minted entry counts for no product, so the coverage check below
		// still compares exactly the catalog's card products against the
		// entries that name one.
		if productID == 0 {
			continue
		}
		if sliceContains(gotFinishes[productID], card.Finish) {
			return out, fmt.Errorf("product %d carries finish %q twice", productID, card.Finish)
		}
		gotFinishes[productID] = append(gotFinishes[productID], card.Finish)
	}
	err := coverage(gotFinishes, wantFinishes)
	if err != nil {
		return out, err
	}
	for productID, want := range wantFinishes {
		got := append([]string(nil), gotFinishes[productID]...)
		sort.Strings(got)
		expected := append([]string(nil), want...)
		sort.Strings(expected)
		if strings.Join(got, "|") != strings.Join(expected, "|") {
			return out, fmt.Errorf("product %d emits finishes %v, skus carry %v", productID, got, expected)
		}
	}
	sealedIDs := map[string]bool{}
	for _, product := range doc.Sealed {
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerID == 0 {
			return out, fmt.Errorf("sealed %q (%s) missing identity", product.Name, product.ID)
		}
		if !idShape.MatchString(product.ID) {
			return out, fmt.Errorf("sealed %q has a uuid nothing can carry: %q", product.Name, product.ID)
		}
		if sealedIDs[product.ID] {
			return out, fmt.Errorf("duplicate sealed id %s", product.ID)
		}
		sealedIDs[product.ID] = true
		if _, found := doc.Sets[product.SetCode]; !found {
			return out, fmt.Errorf("sealed %q in unknown set %s", product.Name, product.SetCode)
		}
	}
	out.sets = len(doc.Sets)
	out.cards = len(doc.Cards)
	out.sealed = len(doc.Sealed)
	return out, nil
}

// checkFabFinishNames refuses a translation table naming a printing the
// catalog does not. fabFinish crosses upstream's edition and foiling codes
// onto TCGplayer's printing names - nothing but knowledge says "N|S" means
// Normal, so the names cannot be derived - and a name the catalog has since
// renamed would mint entries under a printing nobody sells.
func checkFabFinishNames(c *tcgplayer.CatalogDump) {
	listed := map[string]bool{}
	for _, printing := range c.Printings {
		listed[printing.Name] = true
	}
	for code, name := range fabFinish {
		if !listed[name] {
			log.Fatalf("fabFinish maps %q to printing %q, which the catalog does not list", code, name)
		}
	}
}

// printingDisplayOrder is where each of a category's printings sits in the
// order TCGplayer displays them.
func printingDisplayOrder(c *tcgplayer.CatalogDump) map[string]int {
	rank := map[string]int{}
	for _, p := range c.Printings {
		rank[p.Name] = p.DisplayOrder
	}
	return rank
}

// pitchColors are the colours a pitch value names. The card prints the
// value and everyone says the colour, so a query carries "Red" where the
// catalog carries "1".
var pitchColors = map[string]string{"1": "Red", "2": "Yellow", "3": "Blue"}

// pitchInName is the pitch colour a product's name carries, which is the
// game's own way of naming a card: "Snatch (Red)" is a different card from
// "Snatch (Blue)".
var pitchInName = regexp.MustCompile(`\((Red|Yellow|Blue)\)`)

// pitchColor is the colour a product pitches for, empty where it pitches for
// nothing - a hero, an equipment, a token, all of which the catalog writes as
// "0" or "-" - or where the value is not one the game has.
//
// A double-faced card writes the front's value and a bare "//" for the back,
// which pitches for nothing, so the leading value is the card's.
func pitchColor(product tcgplayer.Product) string {
	value, _, _ := strings.Cut(product.Extended("Pitch Value"), "/")
	return pitchColors[strings.TrimSpace(value)]
}

// numberish is a qualifier that is a collector number rather than a promotion:
// the card's own ("Spider's Bite" DYN115 carries "115"), a sibling's
// ("TNP020" beside TNP019), or the bare letter a lettered printing is told
// apart by. A number is what the number field says.
var numberish = regexp.MustCompile(`^[a-z]{0,4}[0-9]{1,4}(-[a-z])?$|^[a-z]$`)

// says reports whether a field's own slug holds a label's, which is how a
// qualifier naming part of what the field already says is recognised: the
// finish "Cold Foil" says the label "Cold".
//
// A label of one or two letters is never read this way. "coldfoil" holds a
// "c" and "fab470" holds an "a", and reading those as the field speaking
// deleted the artwork letter that was the only thing telling three
// printings of Lightning Flow apart - and deleted it from some of them and
// not others, depending on which letters their number happened to contain.
func says(field, label string) bool {
	return field != "" && len(label) > 2 && strings.Contains(field, label)
}

// artworkLetter matches a label that is one letter: which drawing of the
// number this printing carries, where the catalog files several.
var artworkLetter = regexp.MustCompile(`^[A-Za-z]$`)

// promoTypeNames folds the spellings the catalog writes one promotion under.
// Three ways of saying a Japanese alternate art is one promotion, and a query
// naming it should not have to guess which the product name used.
var promoTypeNames = map[string]string{
	"japanese alternate artwork": "japanese alternate art",
	"japanese alternative art":   "japanese alternate art",
	"jpn exclusive":              "japanese exclusive",
	"cc label":                   "cc tag",
}

// subjects are what a printing shows rather than what promoted it: which
// pitch value it is, which hero's deck it came in, which element it depicts,
// and which piece of a puzzle it is. They read like promotions in a product
// name and are none.
//
// A subject is kept where dropping it would leave two printings identical -
// Runechant is ROS162 as Earth and as Lightning, Seismic Surge is MPG112 as
// Crystal, Forest and Lava - because what tells one printing from another is
// exactly what a promo type is for. foldPromoTypes puts those back.
var subjects = map[string]bool{
	// Which piece of a puzzle this one is; the number already says.
	"top left": true, "top center": true, "top right": true,
	"middle left": true, "middle center": true, "middle right": true,
	"bottom left": true, "bottom center": true, "bottom right": true,
	"left": true, "center": true, "right": true,
	// A pitch colour: the card's own is published as its colour and the
	// tag repeating it is dropped above, and another card's - "Yellow
	// FAB385" on the card numbered FAB384, saying where that version is
	// filed - promoted nothing.
	"red": true, "yellow": true, "blue": true, "purple": true,
	// Whose deck it came in.
	"dorinthea": true, "rhinar": true,
	// What it depicts.
	"earth": true, "forest": true, "lava": true, "lightning": true,
	"crystal": true, "maori": true,
}

// foldPromoTypes reduces every card's promo types to the promotions they
// name, and spells each as its slug. It runs once the cards are built because
// the last of it - putting back a subject that is the only thing telling two
// printings apart - is not something one card can see.
func foldPromoTypes(cards []any) (int, int) {
	type held struct {
		item    map[string]any
		kept    []string
		dropped []string
		marks   []string
	}
	var rows []held
	var dropped int
	for _, raw := range cards {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		var row held
		row.item = item
		finish := emit.PromoSlug(fmt.Sprint(item["finish"]))
		number := emit.PromoSlug(fmt.Sprint(item["number"]))
		color := emit.PromoSlug(fmt.Sprint(item["color"]))
		rarity := emit.PromoSlug(fmt.Sprint(item["rarity"]))
		for _, tag := range stringsOf(item["promoTypes"]) {
			if name, found := promoTypeNames[tag]; found {
				tag = name
			}
			slug := emit.PromoSlug(tag)
			switch {
			case slug == "":
			// A qualifier repeating the number, or naming the finish the
			// card already carries, says nothing the card has not said.
			case slug == number || says(number, slug):
				dropped++
			case slug == finish || says(finish, slug):
				dropped++
			// The pitch value is the card's own, published as its colour,
			// and a product name repeating it names no promotion.
			case color != "" && slug == color:
				dropped++
			// Nor does one repeating the rarity. The catalog writes
			// "Marvel" in the product name of the printings it also files
			// at rarity Marvel, and as a tag that declares a printing
			// promotional for being the rarity it is.
			case rarity != "" && slug == rarity:
				dropped++
			// An artwork letter is which copy of the number this is, and
			// nothing promoted a card for being the second drawing of it.
			// Deleting it outright left three printings of Lightning Flow
			// at OMN203 told apart by nothing at all, so it is published
			// as the mark it is.
			//
			// Only a letter. The rest of what numberish matches carries
			// digits, and a label carrying digits is a number - often
			// another printing's, which the catalog writes beside a pitch
			// value to say where that version is filed. Marking a printing
			// with a number that is not its own says the wrong thing.
			case artworkLetter.MatchString(tag):
				row.marks = append(row.marks, strings.ToLower(tag))
			case numberish.MatchString(tag):
				dropped++
			case subjects[tag]:
				row.dropped = append(row.dropped, slug)
			case !slices.Contains(row.kept, slug):
				row.kept = append(row.kept, slug)
			}
		}
		rows = append(rows, row)
	}

	// A subject goes back where the printings it was dropped from are no
	// longer told apart by anything else.
	identity := func(r held) string {
		return fmt.Sprint(r.item["name"], "|", r.item["number"], "|", r.item["setCode"],
			"|", r.item["rarity"], "|", r.item["finish"], "|", r.kept)
	}
	shared := map[string]int{}
	for _, r := range rows {
		shared[identity(r)]++
	}
	tokens := map[string]bool{}
	for _, r := range rows {
		kept := r.kept
		if shared[identity(r)] > 1 {
			kept = append(slices.Clone(kept), r.dropped...)
		} else {
			dropped += len(r.dropped)
		}
		if len(r.marks) > 0 {
			r.item["watermark"] = strings.Join(r.marks, " ")
		}
		if len(kept) == 0 {
			delete(r.item, "promoTypes")
			continue
		}
		slices.Sort(kept)
		out := make([]any, 0, len(kept))
		for _, slug := range kept {
			out = append(out, slug)
			tokens[slug] = true
		}
		r.item["promoTypes"] = out
	}
	return dropped, len(tokens)
}

// stringsOf reads a list of strings back off an entry, which holds them as
// []string before the document is encoded and []any after.
func stringsOf(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, raw := range list {
			if s, ok := raw.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
