// Command fleshandblood builds the Flesh and Blood datastore file consumed
// by go-mtgban's mtgmatcher loader, from the TCGplayer catalog dump for
// category 62 and the-fab-cube's community card dataset.
//
// Identity is the catalog's: every single product is one printing, with an
// id minted from its collector number and product id, so every printing is
// priced by construction and the id space never depends on a cross-source
// join. The catalog's sku printing names (Normal, Rainbow Foil, Cold Foil,
// and the 1st/Unlimited Edition variants of both) are exported per card as
// its finishes: for this game they carry both the finish and the edition
// axis, and TCGplayer folds them into one product instead of splitting.
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
	"sort"
	"strings"
)

const (
	fabCategory = 62

	// tcgSingles is the product type single cards are filed under;
	// everything else is sealed by exclusion.
	tcgSingles = "Cards"

	fabCardsURL = "https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/card-flattened.json"
)

type tcgProduct struct {
	ProductID    int    `json:"productId"`
	Name         string `json:"name"`
	ImageURL     string `json:"imageUrl"`
	GroupID      int    `json:"groupId"`
	ProductType  string `json:"productType"`
	ExtendedData []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"extendedData"`
	Skus []struct {
		PrintingID int `json:"printingId"`
	} `json:"skus"`
}

func (p tcgProduct) extended(name string) string {
	for _, e := range p.ExtendedData {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

type tcgGroup struct {
	GroupID      int    `json:"groupId"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
	PublishedOn  string `json:"publishedOn"`
}

func (g tcgGroup) releaseDate() string {
	return strings.SplitN(g.PublishedOn, "T", 2)[0]
}

// tcgCatalog is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.
type tcgCatalog struct {
	Category struct {
		CategoryID int `json:"categoryId"`
	} `json:"category"`
	Printings []struct {
		PrintingID int    `json:"printingId"`
		Name       string `json:"name"`
	} `json:"printings"`
	Groups   []tcgGroup   `json:"groups"`
	Products []tcgProduct `json:"products"`
}

// printingNames maps each product to the sorted printing names it is sold
// under; a printing the catalog does not list for a product is one that
// does not exist.
func (c *tcgCatalog) printingNames() map[int][]string {
	name := map[int]string{}
	for _, p := range c.Printings {
		name[p.PrintingID] = p.Name
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
		sort.Strings(names)
		out[product.ProductID] = names
	}
	return out
}

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

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)

// numParenRe matches a collector number worn as a parenthetical
// ("(DYN069)"), including one that disagrees with the Number field, which
// the catalog's typos produce.
var numParenRe = regexp.MustCompile(`^[A-Z]{2,4}\d{3}(?:-[A-Z]{1,2})?$`)

// dashNumRe matches a collector number worn as a dash suffix. The catalog
// decorates these loosely — double spaces, "FAB 163" with a space inside,
// numbers that disagree with the Number field — so the exact-match strip
// is backed by this shape-based one.
var dashNumRe = regexp.MustCompile(`\s+-\s*[A-Z]{2,6}\s?\d{2,4}(?:-[A-Z]{1,3})?$`)

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgProduct
	number   string
	baseName string
	quals    []string
}

// decompose strips the collector number worn as decoration and pulls the
// parenthetical qualifiers out of the name.
func decompose(p tcgProduct, num string) single {
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
	for {
		stripped := dashNumRe.ReplaceAllString(name, "")
		if stripped == name {
			break
		}
		name = stripped
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
func setCodes(groups []tcgGroup) map[int]string {
	ordered := append([]tcgGroup(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].GroupID < ordered[j].GroupID
	})

	codes := map[int]string{}
	taken := map[string]bool{}
	claim := func(g tcgGroup, code string) {
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

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 9000, "refuse to emit a datastore with fewer card printings")
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
	var catalog tcgCatalog
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

	groupByID := map[int]tcgGroup{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	codes := setCodes(catalog.Groups)
	printings := catalog.printingNames()

	// Split the products: numbered singles become printings, the
	// non-single types become sealed, and the rest is counted out loud.
	var singles []single
	var sealedProducts []tcgProduct
	var unnumbered int
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		num := product.extended("Number")
		if num == "" {
			// Art cards, counters, uncut-sheet pieces: nothing to
			// identify them by, and no id can be minted without a number.
			unnumbered++
			continue
		}
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept, %d unnumbered left out", len(singles), unnumbered)

	// Per collector number: a qualifier every product of the number
	// carries is part of the name (the pitch colors), not a variant. A
	// number with a single product cannot make that call alone, so the
	// epithets learned from the multi-product numbers decide for it.
	byNumber := map[string][]*single{}
	for i := range singles {
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
	for _, group := range catalog.Groups {
		sets[codes[group.GroupID]] = map[string]any{
			"name":        group.Name,
			"releaseDate": group.releaseDate(),
		}
	}

	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	var cards []any
	var finishless int
	for _, s := range singles {
		entry := map[string]any{
			"id":       fmt.Sprintf("%s_%d", strings.ToLower(s.number), s.product.ProductID),
			"name":     s.baseName,
			"number":   s.number,
			"setCode":  codes[s.product.GroupID],
			"rarity":   s.product.extended("Rarity"),
			"finishes": printings[s.product.ProductID],
			"image":    imageURL(s.product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": s.product.ProductID,
			},
		}
		if len(printings[s.product.ProductID]) == 0 {
			finishless++
		}
		if len(s.quals) > 0 {
			entry["variant"] = strings.Join(s.quals, " ")
		}
		if id, found := fabIDs[s.product.ProductID]; found {
			entry["fabId"] = id
		}
		cards = append(cards, entry)
	}
	if finishless > 0 {
		log.Printf("finishes: %d cards without any sku printing", finishless)
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
			"releaseDate": group.releaseDate(),
			"image":       imageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	log.Printf("emitting %d sets, %d cards, %d sealed", len(sets), len(cards), len(sealed))

	doc := map[string]any{
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
	counted, err := validate(buf.Bytes())
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

// validate decodes an encoded datastore and checks its shape: every card
// and sealed product carrying its identity, every id unique within its
// namespace, every referenced set existing.
func validate(data []byte) (counts, error) {
	var doc struct {
		Sets map[string]struct {
			Name string `json:"name"`
		} `json:"sets"`
		Cards []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Number        string `json:"number"`
			SetCode       string `json:"setCode"`
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

	for code, set := range doc.Sets {
		if set.Name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
	}
	cardIDs := map[string]bool{}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" || card.Number == "" || card.ExternalLinks.TcgPlayerId == 0 {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
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
