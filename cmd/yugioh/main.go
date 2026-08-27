// Command yugioh builds the Yu-Gi-Oh datastore file consumed by
// go-mtgban's mtgmatcher/yugioh loader, from the TCGplayer catalog dump
// for category 2, with set release dates enriched from YGOPRODeck's
// cardsets listing.
//
// Identity is the catalog's, one entry per product and sku printing.
// Rarity is the variant axis — the same collector number appears under
// several rarities as separate products — and the edition axis TCGplayer
// prices as separate skus of one product (1st Edition, Unlimited, Limited)
// is separate entries too, each with its own id, priced by construction —
// the editions-as-flags shape this datastore used to publish folded those
// price points onto one id. The id's edition suffix derives from the
// printing name alone, never from which sibling printings exist, so an id
// cannot churn when TCGplayer later adds an edition to a product.
//
// The name parentheticals TCGplayer decorates products with are told
// apart per collector number, the same way cmd/onepiece does: a
// parenthetical every product of a number carries is part of the card's
// name (Speed Duel deck letters, the alternate arts that are the number's
// whole identity); a qualifier that merely restates the product's own
// Rarity — spelled out, shorthanded ("UTR"), or with "Rare" elided
// ("Starfoil") — is dropped as redundant with the rarity field; whatever
// remains is the variant label the matcher narrows on.
//
// Sets are the catalog groups, kept separate per print run (LOB and its
// 25th Anniversary reissue are different sets). Group abbreviations
// repeat and are sometimes blank, but set codes must be unique, so a
// blank abbreviation gets a code minted from the group id and a repeated
// one gets the group id suffixed onto the later group, both logged.
//
// YGOPRODeck contributes only what the catalog lacks: groups TCGplayer
// has no release date for carry the request time as publishedOn instead,
// and those get the date YGOPRODeck knows, joined by abbreviation first
// and set name second, only when the join is unambiguous. cardinfo.php is
// deliberately not fetched and no YGOPRODeck image URL is stored: their
// terms forbid hotlinking, so images are TCGplayer's alone.
//
// Every product the catalog types as a card becomes an entry, and validate
// refuses a build that left one out: a shape nobody has seen yet stops the
// publish instead of vanishing from the datastore. The products the game
// gives no collector number — the field-center tokens, the art and divider
// cards, the oversized promos — are carried on the id their product alone
// mints, the same shape cmd/pokemon files its basic energies under, and
// they are told apart by the set, the rarity and the variant label the
// product name spells out.
//
// Sealed products are everything the catalog files outside the singles
// type, same as the other games: by exclusion, so a product type
// TCGplayer adds later lands on the sealed side where it is noticed.
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
	yugiohCategory = 2

	ygoprodeckSetsURL = "https://db.ygoprodeck.com/api/v7/cardsets.php"
)

// tcgSingles are the product types single cards are filed under;
// everything else is sealed by exclusion.
var tcgSingles = tcgplayer.SinglesProductTypes(yugiohCategory)

// finishSuffix maps each sku printing name to the suffix its entry's id
// carries. Any other printing name is a hard failure, because a suffix
// invented on the fly would not be a stable identity — the category's
// fourth printing, Normal, appears only on sealed skus and stays out.
var finishSuffix = map[string]string{
	"1st Edition": "_1e",
	"Unlimited":   "_unl",
	"Limited":     "_lim",
}

// finishOrder fixes the order a product's entries are emitted in.
var finishOrder = []string{
	"1st Edition",
	"Unlimited",
	"Limited",
}

// hasDate reports whether the group's publishedOn is a real date: the
// catalog stamps the request time on groups it has no date for, so a
// genuine value is always a bare midnight timestamp.
func hasDate(g tcgplayer.Group) bool {
	return strings.HasSuffix(g.PublishedOn, "T00:00:00")
}

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the distinct printing names its skus
// carry, in finishOrder; a printing the catalog does not list for a product
// is one that does not exist.

// ygoSet is the slice of a YGOPRODeck cardsets entry this build reads.
type ygoSet struct {
	Name string `json:"set_name"`
	Code string `json:"set_code"`
	Date string `json:"tcg_date"`
}

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

// idBase mints the id stem an entry's edition suffix hangs off: the
// collector number and the product id, or the product id alone for a
// product the game gives no number.
func idBase(num string, productID int) string {
	if num == "" {
		return strconv.Itoa(productID)
	}
	return strings.ToLower(num) + "_" + strconv.Itoa(productID)
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

// normalizeName reduces a set name to what two spellings share, so the
// catalog's "Labyrinth_of_Nightmare" still finds YGOPRODeck's "Labyrinth
// of Nightmare".
func normalizeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rarityShorthand spells out the abbreviations TCGplayer decorates
// product names with; each is honored only when the expansion matches the
// product's own Rarity, so a wrong or drifting entry cannot misfold.
var rarityShorthand = map[string]string{
	"UR":  "Ultra Rare",
	"UTR": "Ultimate Rare",
	"SR":  "Super Rare",
	"CR":  "Collector's Rare",
	"PSR": "Prismatic Secret Rare",
	"PUR": "Prismatic Ultimate Rare",
	"PCR": "Prismatic Collector's Rare",
	"EUR": "Emblazoned Ultra Rare",
	"ESR": "Emblazoned Secret Rare",
}

// normRarity folds the case, spacing and apostrophe variants two
// spellings of the same rarity differ by.
func normRarity(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(s, "’", "'"))), " ")
}

// restatesRarity reports whether a name qualifier merely repeats the
// product's Rarity: spelled out, shorthanded, or with the trailing
// "Rare" elided ("Starfoil", "Secret").
func restatesRarity(qual, rarity string) bool {
	r := normRarity(rarity)
	if r == "" {
		return false
	}
	q := normRarity(qual)
	return q == r || q+" rare" == r || normRarity(rarityShorthand[qual]) == r
}

// isPromoGroup reports whether a catalog group hands out promotional
// printings. The group name is the only thing that says so in this
// category: Yu-Gi-Oh rarities name the foil treatment ("Secret Rare",
// "Starfoil Rare") and never the promotion, so the 732 Duelist League
// promos carry no promotional rarity at all. Reading the name also keeps
// the collector tins out, which reprint at retail rather than hand out.
func isPromoGroup(group tcgplayer.Group) bool {
	return strings.Contains(strings.ToLower(group.Name), "promo")
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
var bareNumRe = regexp.MustCompile(`^\d{1,4}$`)

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []string
}

// decompose strips the collector number worn as decoration (a dash
// suffix, a parenthetical repeat, a bare numeric parenthetical) and the
// qualifiers that only restate the product's Rarity, keeping the rest for
// the name-versus-variant call made per collector number below.
func decompose(p tcgplayer.Product, num string) single {
	name := p.Name
	name = strings.ReplaceAll(name, " - "+num, "")
	rarity := p.Extended("Rarity")

	var quals []string
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if bareNumRe.MatchString(q) || strings.EqualFold(q, num) || restatesRarity(q, rarity) {
			return ""
		}
		quals = append(quals, q)
		return ""
	})
	return single{
		product:  p,
		number:   num,
		baseName: strings.Join(strings.Fields(name), " "),
		quals:    quals,
	}
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

// nonCodeRe matches the runs a set code cannot carry.
var nonCodeRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// setCodeOf reduces a catalog abbreviation to what a search query can carry.
// A set code is typed after "is:", and a query is split on whitespace before
// a filter ever sees it and on the colon that names the filter, so a code
// holding either cannot be asked for: "is:OP11 RE" reaches the filter as
// "is:OP11" and "is:crz:gg" names a filter called crz. Every run of anything
// but a letter or a digit becomes one dash, and the ends are trimmed of them.
func setCodeOf(abbreviation string) string {
	return strings.Trim(nonCodeRe.ReplaceAllString(abbreviation, "-"), "-")
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 55000, "refuse to emit a datastore with fewer card entries")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 2 (required)")
	ygoSets := flag.String("ygoprodeck-sets", ygoprodeckSetsURL, "YGOPRODeck cardsets file, path or URL")
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
	if catalog.Category.CategoryID != yugiohCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, yugiohCategory)
	}

	setsData, err := fetch(*ygoSets)
	if err != nil {
		log.Fatalln("ygoprodeck sets:", err)
	}
	var ygo []ygoSet
	if err := json.Unmarshal(setsData, &ygo); err != nil {
		log.Fatalln("ygoprodeck sets:", err)
	}
	// Index the dates by code and by normalized name. Several YGOPRODeck
	// entries share a code (a set beside its special editions), so a key
	// maps to every distinct date it was seen with, and only a key with
	// exactly one is trusted for a fill.
	datesByCode := map[string][]string{}
	datesByName := map[string][]string{}
	addDate := func(index map[string][]string, key, date string) {
		if key == "" || sliceContains(index[key], date) {
			return
		}
		index[key] = append(index[key], date)
	}
	for _, set := range ygo {
		if set.Date == "" {
			continue
		}
		addDate(datesByCode, strings.ToUpper(set.Code), set.Date)
		addDate(datesByName, normalizeName(set.Name), set.Date)
	}
	log.Printf("catalog: %d groups, %d products; ygoprodeck: %d sets over %d codes",
		len(catalog.Groups), len(catalog.Products), len(ygo), len(datesByCode))

	// lookup finds the YGOPRODeck dates for a group: abbreviation match
	// first (whole, then the prefix ahead of the language tail "LOB-EN"
	// carries), set name second.
	lookup := func(g tcgplayer.Group) (dates []string, how string) {
		abbr := strings.ToUpper(g.Abbreviation)
		if abbr != "" {
			dates = datesByCode[abbr]
			if dates == nil {
				dates = datesByCode[strings.SplitN(abbr, "-", 2)[0]]
			}
			if dates != nil {
				return dates, "code"
			}
		}
		dates = datesByName[normalizeName(g.Name)]
		if dates != nil {
			return dates, "name"
		}
		return nil, ""
	}

	// Assign every group its set code, in group-id order so the original
	// print run keeps the bare abbreviation and the reissue gets marked. A
	// blank abbreviation gets a code minted from the group id.
	groups := append([]tcgplayer.Group(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GroupID < groups[j].GroupID
	})
	setCodes := map[int]string{}
	usedCodes := map[string]bool{}
	var minted, suffixed int
	for _, group := range groups {
		code := setCodeOf(group.Abbreviation)
		if code == "" {
			code = fmt.Sprintf("G%d", group.GroupID)
			minted++
			log.Printf("%s: no abbreviation, set code %s minted", group.Name, code)
		}
		if usedCodes[code] {
			code = fmt.Sprintf("%s-%d", code, group.GroupID)
			suffixed++
			log.Printf("%s: abbreviation %s already taken, set code %s minted",
				group.Name, group.Abbreviation, code)
		}
		if usedCodes[code] {
			log.Fatalf("set code %s still not unique; refusing to guess further", code)
		}
		usedCodes[code] = true
		setCodes[group.GroupID] = code
	}
	log.Printf("set codes: %d minted for blank abbreviations, %d deduplicated", minted, suffixed)

	// Resolve every group's release date: publishedOn is authoritative
	// when real, the unambiguous YGOPRODeck date fills the placeholders,
	// and what neither source can date stays empty rather than guessed.
	releaseDates := map[int]string{}
	var joinedByCode, joinedByName, placeholders, filled, unfilled int
	for _, group := range groups {
		dates, how := lookup(group)
		switch how {
		case "code":
			joinedByCode++
		case "name":
			joinedByName++
		}
		if hasDate(group) {
			releaseDates[group.GroupID] = group.ReleaseDate()
			continue
		}
		placeholders++
		if len(dates) == 1 {
			releaseDates[group.GroupID] = dates[0]
			filled++
			log.Printf("%s (%s): release date %s filled from ygoprodeck by %s",
				group.Name, setCodes[group.GroupID], dates[0], how)
			continue
		}
		unfilled++
		log.Printf("%s (%s): no release date and %d ygoprodeck candidates, left undated",
			group.Name, setCodes[group.GroupID], len(dates))
	}
	log.Printf("ygoprodeck join: %d of %d groups (%d by code, %d by name); %d placeholder dates, %d filled, %d left empty",
		joinedByCode+joinedByName, len(groups), joinedByCode, joinedByName,
		placeholders, filled, unfilled)

	printings := printingNames(&catalog)

	// Split the products: every single becomes card entries, the
	// non-single types become sealed. "N/A" is the catalog's spelling for
	// a product with no number, and a product with none is still a card.
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
		if strings.EqualFold(num, "N/A") {
			num = ""
		}
		if num == "" {
			unnumbered++
		}
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept (%d without a collector number)", len(singles), unnumbered)
	if len(singles) == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}

	// Per collector number within its group: a qualifier every product of
	// the number carries is part of the name, not a variant. A number with
	// a single product cannot make that call alone, so the name parts
	// learned from the multi-product numbers decide for it — the same
	// deck letter or epithet decorates the number's every printing.
	byNumber := map[string][]*single{}
	for i := range singles {
		// The unnumbered products of a group are unrelated cards, not one
		// card's printings, so they elect nothing together: they take the
		// verdicts the real numbers reached, as a lone printing does.
		if singles[i].number == "" {
			continue
		}
		key := fmt.Sprintf("%d|%s", singles[i].product.GroupID, singles[i].number)
		byNumber[key] = append(byNumber[key], &singles[i])
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

	// Emit. Sets are the catalog groups; ids embed the product id so they
	// survive any upstream renumbering.
	sets := map[string]any{}
	var promoSets int
	for _, group := range groups {
		set := map[string]any{
			"name":        group.Name,
			"releaseDate": releaseDates[group.GroupID],
		}
		if isPromoGroup(group) {
			set["type"] = "promo"
			promoSets++
		}
		sets[setCodes[group.GroupID]] = set
	}
	log.Printf("promotional sets: %d of %d", promoSets, len(groups))

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

	var cards []any
	for _, s := range singles {
		cardType := s.product.Extended("Card Type")
		if cardType == "" {
			cardType = s.product.Extended("MonsterType")
		}
		productID := s.product.ProductID
		for _, finish := range printings[productID] {
			suffix, known := finishSuffix[finish]
			if !known {
				log.Fatalf("product %d carries printing %q, not one of the three this identity scheme knows",
					productID, finish)
			}
			entry := map[string]any{
				"id":        idBase(s.number, productID) + suffix,
				"name":      s.baseName,
				"setCode":   setCodes[s.product.GroupID],
				"rarity":    s.product.Extended("Rarity"),
				"attribute": s.product.Extended("Attribute"),
				"type":      cardType,
				"finish":    finish,
				"image":     imageURL(s.product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if s.number != "" {
				entry["number"] = s.number
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
				// The same labels as a list: joined, "OTS Stamp Blue"
				// cannot be read back into the two tags it holds, and the
				// matcher needs them whole to declare and to match on.
				entry["promoTypes"] = lowered(s.quals)
			}
			cards = append(cards, entry)
		}
	}

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []any
	for _, product := range sealedProducts {
		code := setCodes[product.GroupID]
		sealed = append(sealed, map[string]any{
			"id":          fmt.Sprintf("%s-%d", strings.ToLower(code), product.ProductID),
			"name":        product.Name,
			"setCode":     code,
			"releaseDate": releaseDates[product.GroupID],
			"image":       imageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	log.Printf("emitting %d sets, %d card entries over %d products, %d sealed",
		len(sets), len(cards), len(singles), len(sealed))
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	doc := map[string]any{
		"game":   "yugioh",
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
	// mtgmatcher/yugioh reads, duplicated so this repository depends on
	// nothing.
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
// and sealed product carrying its identity — for a card that includes the
// rarity it is varied by and the edition its skus price — every id unique
// within its namespace, no two products wearing the same identity, every
// referenced set existing, every finish one of the three printing names,
// and every product's entries covering exactly the sku printings the
// catalog lists for it.
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
			Rarity        string `json:"rarity"`
			Variant       string `json:"variant"`
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

	if doc.Game != "yugioh" {
		return out, fmt.Errorf("game is %q, not yugioh", doc.Game)
	}
	for code, set := range doc.Sets {
		if code == "" || set.Name == "" {
			return out, fmt.Errorf("set %q missing its identity", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cardIDs := map[string]bool{}
	// A query resolves a card by its name, number, set, rarity and variant
	// label, never by the id, so two products wearing all five alike are one
	// card to every consumer and would alias each other's prices. Rarity is
	// in the key because it is the axis this game varies on: a number is
	// reprinted across rarities as separate products carrying one name and
	// no variant label of their own, and the matcher narrows on the rarity
	// to tell them apart, so the four axes the other games identify by would
	// call thousands of those reprints one card. The key holds the product
	// id rather than a flag so a product's own edition entries pass while
	// two different products never do - keying on the finish instead would
	// wave through exactly the pair this is meant to catch, since most
	// products carry a single edition.
	identities := map[string]int{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" || card.Rarity == "" ||
			card.Finish == "" || card.ExternalLinks.TcgPlayerId == 0 {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q", card.Name, card.ID, card.Number)
		}
		if _, known := finishSuffix[card.Finish]; !known {
			return out, fmt.Errorf("card %q (%s) carries unknown finish %q", card.Name, card.ID, card.Finish)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		identity := strings.Join([]string{
			card.Name, card.Number, card.SetCode, card.Rarity, card.Variant}, "|")
		if other, seen := identities[identity]; seen && other != card.ExternalLinks.TcgPlayerId {
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

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
