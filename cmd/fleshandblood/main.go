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
	"github.com/mtgban/go-tcgplayer"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	fabCategory = 62

	// tcgSingles is the product type single cards are filed under;
	// everything else is sealed by exclusion.
	tcgSingles = "Cards"

	// englishLanguage is the catalog's language id for English, the one a
	// product needs a sku in to be part of the English program.
	englishLanguage = 1

	fabCardsURL = "https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/card-flattened.json"
)

// finishSuffix maps each sku printing name to the suffix its entry's id
// carries: the edition prefix (1e, unl, or none) glued to the treatment
// (rainbow, cold, or none), so plain Normal is the bare id. Any other
// printing name is a hard failure, because a suffix invented on the fly
// would not be a stable identity.
var finishSuffix = map[string]string{
	"Normal":                         "",
	"Rainbow Foil":                   "_rainbow",
	"Cold Foil":                      "_cold",
	"1st Edition Normal":             "_1e",
	"1st Edition Rainbow Foil":       "_1erainbow",
	"1st Edition Cold Foil":          "_1ecold",
	"Unlimited Edition Normal":       "_unl",
	"Unlimited Edition Rainbow Foil": "_unlrainbow",
}

// finishOrder fixes the order a product's entries are emitted in.
var finishOrder = []string{
	"Normal",
	"Rainbow Foil",
	"Cold Foil",
	"1st Edition Normal",
	"1st Edition Rainbow Foil",
	"1st Edition Cold Foil",
	"Unlimited Edition Normal",
	"Unlimited Edition Rainbow Foil",
}

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the distinct printing names its skus
// carry, in finishOrder; a printing the catalog does not list for a product
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
// game's own printing id and the TCGplayer product the row maps it to.
type fabRow struct {
	ID        string `json:"id"`
	ProductID string `json:"tcgplayer_product_id"`
}

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

// idBase mints the id stem an entry's finish suffix hangs off: the
// collector number and the product id, or the product id alone for a
// product the game gives no number.
func idBase(num string, productID int) string {
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
		if product.ProductType != tcgSingles {
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
		if g.Abbreviation != "" {
			claim(g, g.Abbreviation)
		}
	}
	for _, g := range ordered {
		if g.Abbreviation != "" {
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

	rank := map[string]int{}
	for i, n := range finishOrder {
		rank[n] = i
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
		sort.Slice(names, func(i, j int) bool {
			ri, iKnown := rank[names[i]]
			rj, jKnown := rank[names[j]]
			if iKnown && jKnown {
				return ri < rj
			}
			if iKnown != jKnown {
				return iKnown
			}
			return names[i] < names[j]
		})
		out[product.ProductID] = names
	}
	return out
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 15000, "refuse to emit a datastore with fewer card entries")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 62 (required)")
	fabCards := flag.String("fab-cards", fabCardsURL, "the-fab-cube card-flattened file, path or URL")
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
	printings := printingNames(&catalog)

	// Split the products: every single becomes printings, the non-single
	// types become sealed.
	var singles []single
	var sealedProducts []tcgplayer.Product
	var unnumbered int
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
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
	fabIDsByProduct := map[int][]string{}
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

	// Emit. Sets are the catalog groups under their repaired codes; ids
	// embed the product id so they survive any upstream renumbering.
	sets := map[string]any{}
	promoted := promoGroups(catalog)
	var promoSets int
	for _, group := range catalog.Groups {
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
	log.Printf("promotional sets: %d of %d", promoSets, len(catalog.Groups))

	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	// The coverage contract: every product the catalog types as a card,
	// with the sku printings it is sold in. validate reads it back off the
	// encoded output, so a product no rule here carried fails the build
	// instead of quietly leaving the datastore.
	catalogFinishes := map[int][]string{}
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
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
			suffix, known := finishSuffix[finish]
			if !known {
				log.Fatalf("product %d carries printing %q, not one of the eight this identity scheme knows",
					productID, finish)
			}
			entry := map[string]any{
				"id":      idBase(s.number, productID) + suffix,
				"name":    s.baseName,
				"setCode": codes[s.product.GroupID],
				"rarity":  s.product.Extended("Rarity"),
				"finish":  finish,
				"image":   imageURL(s.product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if s.number != "" {
				entry["number"] = s.number
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
			"image":       imageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	log.Printf("emitting %d sets, %d card entries over %d products (%d not in English), %d sealed",
		len(sets), len(cards), len(singles), nonEnglish, len(sealed))
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	doc := map[string]any{
		"game":   "fleshandblood",
		"sets":   sets,
		"cards":  cards,
		"sealed": sealed,
	}
	var buf bytes.Buffer
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
	if counted.cards < *minCards {
		log.Fatalf("only %d cards (minimum %d); refusing to publish", counted.cards, *minCards)
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
			ExternalLinks struct {
				TcgPlayerId int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"cards"`
		Sealed []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerId int `json:"tcgPlayerId"`
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
	identities := map[string]int{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" ||
			card.Finish == "" || card.ExternalLinks.TcgPlayerId == 0 {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if _, known := finishSuffix[card.Finish]; !known {
			return out, fmt.Errorf("card %q (%s) carries unknown finish %q", card.Name, card.ID, card.Finish)
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
		other, seen := identities[identity]
		if seen && other != card.ExternalLinks.TcgPlayerId {
			return out, fmt.Errorf("products %d and %d wear one identity: %s",
				other, card.ExternalLinks.TcgPlayerId, identity)
		}
		identities[identity] = card.ExternalLinks.TcgPlayerId
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		productID := card.ExternalLinks.TcgPlayerId
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
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerId == 0 {
			return out, fmt.Errorf("sealed %q (%s) missing identity", product.Name, product.ID)
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
