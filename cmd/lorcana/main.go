// Command lorcana-datastore builds the Lorcana datastore file consumed by
// go-mtgban's mtgmatcher/lorcana loader: it takes the LorcanaJSON allCards
// payload and merges in what our TCGplayer catalog dump for category 71
// knows and it does not.
//
// Unlike Riftbound, where the official gallery says nothing at all about
// commerce, LorcanaJSON already carries a TCGplayer product id for 99.6% of
// its cards, so the card-side merge is deliberately narrow:
//
//   - it takes a product id upstream put on two cards away from the card
//     the product does not identify, since one printing belongs to one
//     card and a shared id merges two cards' price histories;
//   - it fills the product id on cards that have none, when exactly one
//     unclaimed catalog product matches by name and collector number;
//   - it records the extra product ids TCGplayer uses for a card's foil,
//     which it sells as a separate product, so a feed keyed on those ids
//     resolves to the card instead of being dropped;
//   - it exports the TCGplayer printing names each card is sold under
//     (Normal, Holofoil, Cold Foil), beside LorcanaJSON's own richer foil
//     sub-types, and reports where the two sources disagree.
//
// The promotional printings TCGplayer files in their own groups (DLPC, D23,
// D100) are matched onto upstream's own cards wherever the id fill above
// can do it by name and number, because upstream files them under the set
// they belong to and its card is the better one. What no card claims or
// matches is minted here rather than dropped: a product TCGplayer sells is
// a printing that exists, and a datastore leaving it out leaves every
// listing of it unresolvable.
//
// A minted card is filed under the negated product id. LorcanaJSON's ids
// are positive counting numbers, so the negative half of the integer space
// is unmistakably ours and cannot collide with an id upstream publishes
// later however far its numbering runs — which is what kept these products
// out before — and the product a card was minted from reads straight off
// its id. Everything else a minted card carries is the catalog's own word:
// the product name, the group's set code — its abbreviation, or the
// abbreviation with the group id suffixed where an earlier group already
// claimed it, so two groups can never fold onto one set — the collector
// number where there is one and 0 where there is none, the rarity, the
// printings as foil types, and the language for a printing sold in no
// English sku. The day upstream publishes the real card, its own entry
// claims the product id and the minted one stops being minted.
//
// Sealed products are appended in full: everything the catalog files
// outside the singles type, in a top-level "sealed" array a stock
// LorcanaJSON reader ignores, with a set entry minted for the groups
// LorcanaJSON has no set for.
//
// Card identity is left entirely to LorcanaJSON. Its integer card ids are
// the matcher's uuids and are quoted directly in chart URLs, and its foil
// sub-type names ("Silver", "RainbowPillars", …) are what
// mtgmatcher/lorcana's selectFinish resolves storefront wording against;
// TCGplayer knows only Normal/Holofoil/Cold Foil and can reproduce neither.
//
// The output is the LorcanaJSON payload itself with the extra data merged
// in, so the loader reads it unchanged and a stock LorcanaJSON reader still
// parses it — and it is re-read and structurally verified before being
// written, so a broken upstream payload can never be published.
//
// This repository is deliberately standalone: it produces JSON and depends
// on nothing, so a datastore change never waits on a go-mtgban release.
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
	"unicode"
)

const (
	// lorcanaCategory is Lorcana's TCGplayer category, the one the catalog
	// dump is expected to carry.
	lorcanaCategory = 71

	// englishLanguage is the catalog's language id for English, the one a
	// product needs a sku in to be part of the English program.
	englishLanguage = 1
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game. Everything else the catalog carries is
// a sealed product: the comparison is against the singles types rather
// than a list of sealed ones, so a type TCGplayer adds later lands on the
// sealed side where it is noticed instead of silently passing as a single.
var tcgSingles = tcgplayer.SinglesProductTypes(lorcanaCategory)

// releaseDate reduces a group's publishedOn timestamp to the bare day
// LorcanaJSON dates carry ("2023-08-18T00:00:00" -> "2023-08-18").

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the sorted printing names it is sold
// under. TCGplayer's category 71 has exactly three — Normal, Holofoil and
// Cold Foil — and a printing it does not list for a product is one that
// does not exist.
func printingNames(c *tcgplayer.CatalogDump) map[int][]string {
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

// foilOnly reports whether every printing a product is sold in is a foil
// one.
func foilOnly(printings []string) bool {
	if len(printings) == 0 {
		return false
	}
	for _, name := range printings {
		if name == "Normal" {
			return false
		}
	}
	return true
}

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

// number reduces a collector number to the loader's canonical form: what
// precedes any "/total" tail, without leading zeros. An all-zero number stays
// "0", because a genuine 0-numbered promo exists.
func number(code string) string {
	code = strings.Split(code, "/")[0]
	trimmed := strings.TrimLeft(code, "0")
	if trimmed == "" && code != "" {
		return "0"
	}
	return trimmed
}

// setCodes assigns every catalog group the set code its minted cards and
// sealed products are filed under: its own abbreviation, which is what
// LorcanaJSON calls the set where upstream carries one, and the
// abbreviation with the group id suffixed where an earlier group already
// claimed it. Abbreviations repeat across groups in every other category,
// and a second group filed under a code the first already holds had its
// name, its date and its whole identity folded onto that first group's set
// - silently, because the code still resolved for every card naming it.
// Codes are claimed in group-id order, so the group that claimed one keeps
// it bare and only the later arrival is marked.
func setCodes(groups []tcgplayer.Group) map[int]string {
	ordered := append([]tcgplayer.Group(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].GroupID < ordered[j].GroupID
	})
	codes := map[int]string{}
	used := map[string]bool{}
	for _, group := range ordered {
		code := group.Abbreviation
		if used[code] {
			code = fmt.Sprintf("%s-%d", code, group.GroupID)
			log.Printf("%s: abbreviation %s already taken, set code %s minted",
				group.Name, group.Abbreviation, code)
		}
		used[code] = true
		codes[group.GroupID] = code
	}
	return codes
}

// mintedID is the card id given to a printing upstream does not carry: the
// negated product id. LorcanaJSON's ids are positive counting numbers, so
// the negative half of the space is unmistakably ours and cannot collide
// with an id upstream publishes later, however far its own numbering runs -
// and the product the card was minted from reads straight off it.
func mintedID(productID int) int {
	return -productID
}

// mintedNumber splits a catalog collector number into the integer upstream
// files a card under and the letter tail it calls the card's variant
// ("25a"), reading through the "/total" tail the catalog writes. A product
// with no number at all is filed under 0, as the numberless promos are.
func mintedNumber(code string) (int, string) {
	digits := number(code)
	i := 0
	for i < len(digits) && digits[i] >= '0' && digits[i] <= '9' {
		i++
	}
	num, _ := strconv.Atoi(digits[:i])
	return num, digits[i:]
}

// foilTypes names the finishes a minted card is sold in the way upstream
// names them: "None" for the plain printing, and TCGplayer's own printing
// name for a foil, which is all that is knowable about a card upstream has
// never published. The loader reads every name but "None" as a foil.
func foilTypes(printings []string) []string {
	var types []string
	for _, name := range printings {
		if name == "Normal" {
			types = append(types, "None")
			continue
		}
		types = append(types, name)
	}
	return types
}

// productLanguage names the language a product is printed in, empty for the
// English program: a product TCGplayer prices in no English sku is sold in
// another language, and the catalog's own language list spells out which.
// Several non-English languages on one product would be a shape this has
// never seen, so it is said out loud and the lowest id wins.
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

// normalizeName reduces a name to what two spellings of the same card share:
// TCGplayer drops diacritics ("Te Ka" for "Te Kā") and appends storefront
// decoration in parentheses, neither of which is part of the card's identity.
func normalizeName(name string) string {
	if idx := strings.IndexByte(name, '('); idx >= 0 {
		name = name[:idx]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fetch reads a local path, or an http(s) URL when one is given. The
// LorcanaJSON download location is deliberately not hardcoded: CI already
// holds it in vars.DATASTORE_LORCANA and passes it in, so there is one place
// to change if upstream moves.
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

// card is the handful of fields the merge reads out of a generically decoded
// LorcanaJSON card, so everything else survives the round trip untouched.
type card struct {
	raw       map[string]any
	links     map[string]any
	fullName  string
	setCode   string
	number    string
	tcgID     int
	foilTypes []string
}

func decodeCard(item any) (card, bool) {
	raw, ok := item.(map[string]any)
	if !ok {
		return card{}, false
	}
	name, _ := raw["fullName"].(string)
	setCode, _ := raw["setCode"].(string)
	num, _ := raw["number"].(float64)
	if name == "" || setCode == "" {
		return card{}, false
	}

	// externalLinks is present on every card in practice; create it rather
	// than skip the card, so a card missing it can still be given an id.
	links, ok := raw["externalLinks"].(map[string]any)
	if !ok {
		links = map[string]any{}
		raw["externalLinks"] = links
	}
	id, _ := links["tcgPlayerId"].(float64)

	var foilTypes []string
	if types, ok := raw["foilTypes"].([]any); ok {
		for _, t := range types {
			if s, ok := t.(string); ok {
				foilTypes = append(foilTypes, s)
			}
		}
	}

	return card{
		raw:       raw,
		links:     links,
		fullName:  name,
		setCode:   setCode,
		number:    strconv.Itoa(int(num)),
		tcgID:     int(id),
		foilTypes: foilTypes,
	}, true
}

// datastoreCounts is what a datastore holds: the two totals, and the card
// count per set. It is read off an encoded datastore - this build's own, or
// the one it is about to replace - so both sides are counted the same way
// by the same code.
type datastoreCounts struct {
	cards, sealed int
	bySet         map[string]int
}

func countDatastore(data []byte) (datastoreCounts, error) {
	var doc struct {
		Cards []struct {
			SetCode string `json:"setCode"`
		} `json:"cards"`
		Sealed []json.RawMessage `json:"sealed"`
	}
	out := datastoreCounts{bySet: map[string]int{}}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	out.cards = len(doc.Cards)
	out.sealed = len(doc.Sealed)
	for _, card := range doc.Cards {
		out.bySet[card.SetCode]++
	}
	return out, nil
}

// regression compares this build against the datastore it is about to
// replace and refuses to publish one that lost a meaningful share of it.
// The minimum card count this used to be checked against was a number
// invented once and never revisited, far below what the datastore actually
// holds, so a build could lose a third of itself and still publish. The
// previous datastore is the number that keeps itself up to date.
//
// Only shrinkage is suspicious - these datastores grow every week - and
// only three shapes of it are refused: a total that fell by more than the
// tolerance, a set that holds no card at all any more, and a set that lost
// more than half of what it held. The last two are what a whole-file count
// cannot see: one set folding onto another moves the total by a fraction
// of a percent while emptying a set completely. Every other per-set drop is
// logged rather than refused, because a product delisted here and there is
// ordinary and a build that cried wolf would be turned off.
func regression(previous, current datastoreCounts, tolerance float64) error {
	if previous.cards == 0 {
		return nil
	}
	lost := func(was, now int) bool {
		return now < was && float64(was-now)/float64(was) > tolerance
	}
	if lost(previous.cards, current.cards) {
		return fmt.Errorf("%d cards, down from %d, more than the %.1f%% a build may lose",
			current.cards, previous.cards, tolerance*100)
	}
	if lost(previous.sealed, current.sealed) {
		return fmt.Errorf("%d sealed products, down from %d, more than the %.1f%% a build may lose",
			current.sealed, previous.sealed, tolerance*100)
	}
	var vanished, collapsed, shrank []string
	for code, was := range previous.bySet {
		now := current.bySet[code]
		switch {
		case now == 0:
			vanished = append(vanished, code)
		case now*2 < was:
			collapsed = append(collapsed, fmt.Sprintf("%s %d->%d", code, was, now))
		case now < was:
			shrank = append(shrank, fmt.Sprintf("%s %d->%d", code, was, now))
		}
	}
	sort.Strings(vanished)
	sort.Strings(collapsed)
	sort.Strings(shrank)
	for _, s := range shrank {
		log.Printf("against: set %s", s)
	}
	if len(vanished) > 0 {
		return fmt.Errorf("%d sets hold no card any more: %s",
			len(vanished), strings.Join(vanished, " "))
	}
	if len(collapsed) > 0 {
		return fmt.Errorf("%d sets lost more than half of what they held: %s",
			len(collapsed), strings.Join(collapsed, " "))
	}
	return nil
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 3000, "refuse to emit a datastore with fewer cards")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 71 (required)")
	source := flag.String("lorcana", "", "LorcanaJSON allCards file, path or URL (required)")
	against := flag.String("against", "", "previous datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.02, "the share of its cards or sealed products a build may lose")
	flag.Parse()

	if *catalogPath == "" {
		log.Fatalln("-tcg-catalog is required: the dump carries the product ids")
	}
	if *source == "" {
		log.Fatalln("-lorcana is required: the LorcanaJSON allCards file this enriches")
	}

	catalogData, err := os.ReadFile(*catalogPath)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	var catalog tcgplayer.CatalogDump
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != lorcanaCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, lorcanaCategory)
	}
	productByID := map[int]tcgplayer.Product{}
	// The coverage contract: every product the catalog types as a card.
	// validate reads it back off the encoded output, so a product no rule
	// here carried fails the build instead of leaving the datastore.
	cardProducts := map[int]bool{}
	for _, product := range catalog.Products {
		productByID[product.ProductID] = product
		if slices.Contains(tcgSingles, product.ProductType) {
			cardProducts[product.ProductID] = true
		}
	}
	singles := len(cardProducts)
	// A dump from before the product type was recorded types nothing, and
	// the sealed-by-exclusion rule would then file the whole catalog as
	// sealed; a dump whose singles all vanished is equally implausible.
	if singles == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}
	printings := printingNames(&catalog)
	log.Printf("catalog: %d groups, %d products (%d singles)",
		len(catalog.Groups), len(catalog.Products), singles)

	payload, err := fetch(*source)
	if err != nil {
		log.Fatalln("lorcana source:", err)
	}
	// Decode generically so everything the loader does not care about
	// survives the round trip untouched.
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		log.Fatalln("lorcana source:", err)
	}
	items, _ := doc["cards"].([]any)
	if len(items) == 0 {
		log.Fatalln("lorcana source: no cards")
	}

	var cards []card
	claimed := map[int]bool{}
	claimants := map[int][]int{}
	for _, item := range items {
		c, ok := decodeCard(item)
		if !ok {
			continue
		}
		cards = append(cards, c)
		if c.tcgID != 0 {
			claimed[c.tcgID] = true
			claimants[c.tcgID] = append(claimants[c.tcgID], len(cards)-1)
		}
	}
	log.Printf("lorcana: %d cards, %d already carrying a product id", len(cards), len(claimed))

	// Upstream sometimes puts one product id on two cards, and one printing
	// belongs to one card: a shared id merges their price histories into
	// whichever of them a consumer loads last. Settle it on the product's
	// own name and collector number rather than on upstream's array order,
	// so the answer does not move when upstream reorders — the claimant the
	// product identifies keeps the id, the other loses it and is left to
	// the fill below, which finds it the product that does match. A product
	// identifying none of its claimants or several is left for validate to
	// refuse, because nothing here can tell those cards apart.
	for id, indexes := range claimants {
		if len(indexes) < 2 {
			continue
		}
		product, found := productByID[id]
		if !found {
			continue
		}
		key := normalizeName(product.Name) + "|" + number(product.Extended("Number"))
		var keeps []int
		for _, i := range indexes {
			if normalizeName(cards[i].fullName)+"|"+cards[i].number == key {
				keeps = append(keeps, i)
			}
		}
		if len(keeps) != 1 {
			continue
		}
		for _, i := range indexes {
			if i == keeps[0] {
				continue
			}
			cards[i].tcgID = 0
			delete(cards[i].links, "tcgPlayerId")
			// The url names the same contested product, so it would
			// contradict whatever id the fill gives this card.
			delete(cards[i].links, "tcgPlayerUrl")
			log.Printf("contested product %d: kept on %s (%s %s), dropped from %s (%s %s)",
				id, cards[keeps[0]].fullName, cards[keeps[0]].setCode, cards[keeps[0]].number,
				cards[i].fullName, cards[i].setCode, cards[i].number)
		}
	}

	// Index the single products no card claims, by normalized name and
	// collector number. Both lookups below key on that pair rather than on
	// the group, because TCGplayer files promotional printings in their own
	// groups (DLPC, D23, D100) while LorcanaJSON files them under the set
	// they belong to, so the group never lines up for exactly the cards
	// that need the most help.
	unclaimed := map[string][]tcgplayer.Product{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) || claimed[product.ProductID] {
			continue
		}
		num := product.Extended("Number")
		if num == "" {
			// An unnumbered single (puzzle inserts, lore cards): nothing
			// to identify it by, and the matcher has no concept for it.
			continue
		}
		key := normalizeName(product.Name) + "|" + number(num)
		unclaimed[key] = append(unclaimed[key], product)
	}
	// Stable order, so unchanged data keeps producing byte-identical output.
	for key := range unclaimed {
		products := unclaimed[key]
		sort.Slice(products, func(i, j int) bool {
			return products[i].ProductID < products[j].ProductID
		})
	}

	var filled, extras int
	matched := map[int]bool{}
	// By index: the id filled below has to land on the card itself rather
	// than on a copy of it, or the printing export and the finish audit
	// that follow skip every card this just identified.
	for i := range cards {
		c := &cards[i]
		key := normalizeName(c.fullName) + "|" + c.number
		candidates := unclaimed[key]

		if c.tcgID == 0 {
			// Only an unambiguous match may stand in for an id upstream did
			// not publish: several candidates means we cannot tell which
			// printing is the card's, and a wrong id silently reroutes a
			// card's whole price history.
			if len(candidates) != 1 {
				continue
			}
			c.tcgID = candidates[0].ProductID
			c.links["tcgPlayerId"] = c.tcgID
			matched[c.tcgID] = true
			filled++
			continue
		}

		// TCGplayer sometimes sells a card's foil as its own product, leaving
		// the claimed product foilless; those extra ids resolve to this same
		// printing. The name must match exactly once the decoration is
		// stripped AND the product must be foil-only, which excludes the
		// oversized, errata and region-exclusive listings that share a name
		// and number but are a different object whose prices must not land
		// here.
		var ids []int
		for _, product := range candidates {
			if !foilOnly(printings[product.ProductID]) {
				continue
			}
			if !strings.HasSuffix(product.Name, "(Foil)") {
				continue
			}
			ids = append(ids, product.ProductID)
			matched[product.ProductID] = true
		}
		if len(ids) > 0 {
			c.links["tcgPlayerExtraIds"] = ids
			extras += len(ids)
		}
	}
	log.Printf("merged: %d product ids filled in, %d extra product ids recorded", filled, extras)

	// Export the TCGplayer printing names each card is sold under, the
	// union over its claimed and extra products, beside LorcanaJSON's own
	// richer foil sub-types. Where the two sources disagree on the basic
	// nonfoil/foil split, say so: LorcanaJSON stays authoritative (30 of
	// the 36 historical disagreements were TCGplayer splitting one card
	// into two products), but a disagreement that survives the product
	// join deserves eyes.
	var disagreements int
	for _, c := range cards {
		if c.tcgID == 0 {
			continue
		}
		names := append([]string(nil), printings[c.tcgID]...)
		if extraIds, ok := c.links["tcgPlayerExtraIds"].([]int); ok {
			for _, id := range extraIds {
				for _, n := range printings[id] {
					if !sliceContains(names, n) {
						names = append(names, n)
					}
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		c.links["tcgPrintings"] = names

		var ljNonfoil, ljFoil bool
		for _, t := range c.foilTypes {
			if strings.EqualFold(t, "none") {
				ljNonfoil = true
			} else {
				ljFoil = true
			}
		}
		if len(c.foilTypes) == 0 {
			ljNonfoil = true
		}
		tcgNonfoil := sliceContains(names, "Normal")
		tcgFoil := len(names) > 1 || !tcgNonfoil
		if ljNonfoil != tcgNonfoil || ljFoil != tcgFoil {
			disagreements++
			if disagreements <= 5 {
				log.Printf("finish disagreement: %s (%s %s) upstream %v vs catalog %v",
					c.fullName, c.setCode, c.number, c.foilTypes, names)
			}
		}
	}
	if disagreements > 0 {
		log.Printf("finish disagreements: %d cards (upstream stays authoritative)", disagreements)
	}

	// Mint a card for every single the catalog carries that no card
	// claimed or matched: the printings upstream has not published, the
	// unnumbered inserts and oversized components it has no concept for,
	// and the listings a card's own product join refused. A product
	// TCGplayer sells is a printing that exists, and a datastore leaving
	// it out leaves every listing of it unresolvable.
	groupByID := map[int]tcgplayer.Group{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	languageNames := map[int]string{}
	for _, language := range catalog.Languages {
		languageNames[language.LanguageID] = language.Name
	}
	var mintable []tcgplayer.Product
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		if claimed[product.ProductID] || matched[product.ProductID] {
			continue
		}
		mintable = append(mintable, product)
	}
	sort.Slice(mintable, func(i, j int) bool {
		return mintable[i].ProductID < mintable[j].ProductID
	})
	codes := setCodes(catalog.Groups)
	mintedByGroup := map[string]int{}
	for _, product := range mintable {
		group := groupByID[product.GroupID]
		num, variant := mintedNumber(product.Extended("Number"))
		links := map[string]any{"tcgPlayerId": product.ProductID}
		if names := printings[product.ProductID]; len(names) > 0 {
			links["tcgPrintings"] = names
		}
		item := map[string]any{
			"id":        mintedID(product.ProductID),
			"fullName":  product.Name,
			"name":      product.Name,
			"setCode":   codes[group.GroupID],
			"number":    num,
			"rarity":    product.Extended("Rarity"),
			"foilTypes": foilTypes(printings[product.ProductID]),
			"images": map[string]any{
				"full":      imageURL(product.ImageURL),
				"thumbnail": product.ImageURL,
			},
			"externalLinks": links,
		}
		if variant != "" {
			item["variant"] = variant
		}
		if cardType := product.Extended("CardType"); cardType != "" {
			item["type"] = cardType
		}
		// A printing TCGplayer prices in no English sku is sold in another
		// language; the catalog's own language list says which. The
		// matcher drops a non-English candidate from a query that named no
		// language, so the row exists without English matching changing.
		if language := productLanguage(languageNames, product); language != "" {
			item["language"] = language
		}
		items = append(items, item)
		mintedByGroup[codes[group.GroupID]]++
	}
	doc["cards"] = items
	log.Printf("minted: %d cards for products upstream does not carry, by group %v",
		len(mintable), mintedByGroup)

	// Sealed products: everything the catalog files outside the singles
	// type, from every group, in a top-level array a stock LorcanaJSON
	// reader ignores. Groups LorcanaJSON has no set for (the promotional
	// ones) get a set entry minted so every product's set exists, card
	// and sealed alike.
	groups := append([]tcgplayer.Group(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Abbreviation < groups[j].Abbreviation
	})
	productsByGroup := map[int][]tcgplayer.Product{}
	for _, product := range catalog.Products {
		productsByGroup[product.GroupID] = append(productsByGroup[product.GroupID], product)
	}
	for _, products := range productsByGroup {
		sort.Slice(products, func(i, j int) bool {
			return products[i].ProductID < products[j].ProductID
		})
	}

	sets, _ := doc["sets"].(map[string]any)
	if sets == nil {
		log.Fatalln("lorcana source: no sets")
	}
	var sealedItems []any
	for _, group := range groups {
		var count int
		for _, product := range productsByGroup[group.GroupID] {
			if slices.Contains(tcgSingles, product.ProductType) {
				continue
			}
			sealedItems = append(sealedItems, map[string]any{
				"id":          fmt.Sprintf("%s-%d", strings.ToLower(codes[group.GroupID]), product.ProductID),
				"name":        product.Name,
				"setCode":     codes[group.GroupID],
				"releaseDate": group.ReleaseDate(),
				"image":       imageURL(product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": product.ProductID,
				},
			})
			count++
		}
		code := codes[group.GroupID]
		count += mintedByGroup[code]
		if count == 0 {
			continue
		}
		if _, found := sets[code]; !found {
			sets[code] = map[string]any{
				"name":        group.Name,
				"releaseDate": group.ReleaseDate(),
				"type":        "promo",
			}
			log.Printf("%s (%s): set minted for %d products", group.Name, code, count)
		}
	}
	if len(sealedItems) > 0 {
		doc["sealed"] = sealedItems
	}
	log.Printf("sealed: %d products", len(sealedItems))

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(doc); err != nil {
		log.Fatalln(err)
	}

	// Re-read the encoded output and verify it structurally before
	// publishing anything: an upstream format change or a truncated
	// download must fail here, not in every consumer. The types mirror
	// what go-mtgban's mtgmatcher/lorcana reads, duplicated so this
	// repository depends on nothing.
	counted, err := validate(buf.Bytes(), cardProducts)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d tcgplayer ids, %d sealed",
		counted.sets, counted.cards, counted.identified, counted.sealed)
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped (%d minted, %d upstream)",
		counted.carried, len(cardProducts), len(cardProducts)-counted.carried,
		len(mintable), counted.carried-len(mintable))
	emitted := len(cards) + len(mintable)
	if counted.cards != emitted || counted.sealed != len(sealedItems) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			emitted, len(sealedItems), counted.cards, counted.sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a card, so it is exhaustive by construction
	// and cannot lose a product to a rule that did not know what to do with
	// it - the card side's whole failure mode. What it can lose a product
	// to is an edit: one `continue` on the sealed path and the products
	// would leave the datastore with nothing to say so, the card side's
	// invariant being blind to them. Counting the emitted products back
	// against the catalog total is what says so.
	wantSealed := len(catalog.Products) - singles
	if counted.sealed != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			counted.sealed, wantSealed)
	}
	if counted.cards < *minCards {
		log.Fatalf("only %d cards (minimum %d); refusing to publish", counted.cards, *minCards)
	}

	// Compare against the datastore this build is about to replace, when
	// the publish handed one over. It is the only baseline that keeps
	// itself current, and the one thing an edit in here cannot move.
	if *against != "" {
		previousData, err := os.ReadFile(*against)
		if err != nil {
			log.Fatalln("against:", err)
		}
		previous, err := countDatastore(previousData)
		if err != nil {
			log.Fatalln("against:", err)
		}
		current, err := countDatastore(buf.Bytes())
		if err != nil {
			log.Fatalln("against:", err)
		}
		log.Printf("against %s: %d cards (was %d), %d sealed (was %d), %d sets (was %d)",
			*against, current.cards, previous.cards, current.sealed, previous.sealed,
			len(current.bySet), len(previous.bySet))
		if err := regression(previous, current, *againstTolerance); err != nil {
			log.Fatalln("against: refusing to publish:", err)
		}
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
	sets, cards, sealed, identified, carried int
}

// validate decodes an encoded datastore and checks its shape: sets and
// cards present, every card and sealed product carrying its identity,
// every id unique within its namespace, every sealed set existing, and
// every product the catalog types as a card claimed by a card — the
// zero-skip invariant, checked on the encoded output so a product no rule
// above carried stops the publish instead of leaving the datastore.
// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func validate(data []byte, cardProducts map[int]bool) (counts, error) {
	var doc struct {
		Sets map[string]struct {
			Name        string `json:"name"`
			ReleaseDate string `json:"releaseDate"`
		} `json:"sets"`
		Cards []struct {
			ID            int    `json:"id"`
			FullName      string `json:"fullName"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerId     int   `json:"tcgPlayerId"`
				TcgPlayerExtras []int `json:"tcgPlayerExtraIds"`
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

	// The date is not required: upstream lists future sets before their
	// release date is announced.
	for code, set := range doc.Sets {
		if set.Name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cardIDs := map[int]bool{}
	// A product is one printing sold under one listing, so it belongs to one
	// card: two cards claiming it merge their price histories into whichever
	// of them a consumer happens to load last, which flips the day upstream
	// reorders its array. Extra ids name products in the same namespace and
	// are checked against the same claims.
	claimedBy := map[int]string{}
	for _, card := range doc.Cards {
		if card.ID == 0 || card.FullName == "" || card.SetCode == "" {
			return out, fmt.Errorf("card %q (%d) missing identity", card.FullName, card.ID)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %d", card.ID)
		}
		cardIDs[card.ID] = true
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.FullName, card.SetCode)
		}
		claimant := fmt.Sprintf("%q (%d)", card.FullName, card.ID)
		for _, id := range append([]int{card.ExternalLinks.TcgPlayerId}, card.ExternalLinks.TcgPlayerExtras...) {
			if id == 0 {
				continue
			}
			if previous, found := claimedBy[id]; found {
				return out, fmt.Errorf("tcgplayer product %d claimed by both %s and %s", id, previous, claimant)
			}
			claimedBy[id] = claimant
		}
		if card.ExternalLinks.TcgPlayerId != 0 {
			out.identified++
		}
	}
	var missing, foreign []int
	for id := range claimedBy {
		if !cardProducts[id] {
			foreign = append(foreign, id)
			continue
		}
		out.carried++
	}
	for id := range cardProducts {
		if _, found := claimedBy[id]; !found {
			missing = append(missing, id)
		}
	}
	sort.Ints(missing)
	sort.Ints(foreign)
	if len(missing) > 0 {
		return out, fmt.Errorf("%d catalog card products carry no card, first is %d",
			len(missing), missing[0])
	}
	// A claim naming no card product is upstream's to make, not this
	// build's: the dump can lag a day behind a product upstream already
	// links, so it is said out loud rather than refused.
	if len(foreign) > 0 {
		log.Printf("%d claimed product ids the catalog types as no card, first is %d",
			len(foreign), foreign[0])
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
