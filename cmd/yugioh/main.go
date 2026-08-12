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
// Sealed products are everything the catalog files outside the singles
// type, same as the other games: by exclusion, so a product type
// TCGplayer adds later lands on the sealed side where it is noticed.
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
	yugiohCategory = 2

	// tcgSingles is the product type single cards are filed under;
	// everything else is sealed by exclusion.
	tcgSingles = "Cards"

	ygoprodeckSetsURL = "https://db.ygoprodeck.com/api/v7/cardsets.php"
)

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

// hasDate reports whether the group's publishedOn is a real date: the
// catalog stamps the request time on groups it has no date for, so a
// genuine value is always a bare midnight timestamp.
func (g tcgGroup) hasDate() bool {
	return strings.HasSuffix(g.PublishedOn, "T00:00:00")
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

// printingNames maps each product to the distinct printing names its skus
// carry, in finishOrder; a printing the catalog does not list for a product
// is one that does not exist.
func (c *tcgCatalog) printingNames() map[int][]string {
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

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)
var bareNumRe = regexp.MustCompile(`^\d{1,4}$`)

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgProduct
	number   string
	baseName string
	quals    []string
}

// decompose strips the collector number worn as decoration (a dash
// suffix, a parenthetical repeat, a bare numeric parenthetical) and the
// qualifiers that only restate the product's Rarity, keeping the rest for
// the name-versus-variant call made per collector number below.
func decompose(p tcgProduct, num string) single {
	name := p.Name
	name = strings.ReplaceAll(name, " - "+num, "")
	rarity := p.extended("Rarity")

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
	var catalog tcgCatalog
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
	lookup := func(g tcgGroup) (dates []string, how string) {
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
	groups := append([]tcgGroup(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GroupID < groups[j].GroupID
	})
	setCodes := map[int]string{}
	usedCodes := map[string]bool{}
	var minted, suffixed int
	for _, group := range groups {
		code := group.Abbreviation
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
		if group.hasDate() {
			releaseDates[group.GroupID] = group.releaseDate()
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

	printings := catalog.printingNames()

	// Split the products: numbered singles become card entries, the
	// non-single types become sealed, and the rest is counted out loud.
	// "N/A" is the catalog's spelling for a product with no number.
	var singles []single
	var sealedProducts []tcgProduct
	var unnumbered, printingless int
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		num := product.extended("Number")
		if num == "" || strings.EqualFold(num, "N/A") {
			unnumbered++
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			printingless++
			log.Printf("no sku printing: %q (%d) left out", product.Name, product.ProductID)
			continue
		}
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept, %d without a collector number and %d printingless left out",
		len(singles), unnumbered, printingless)
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

	// Emit. Sets are the catalog groups; ids embed the product id so they
	// survive any upstream renumbering.
	sets := map[string]any{}
	for _, group := range groups {
		sets[setCodes[group.GroupID]] = map[string]any{
			"name":        group.Name,
			"releaseDate": releaseDates[group.GroupID],
		}
	}

	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	var cards []any
	wantFinishes := map[int][]string{}
	for _, s := range singles {
		cardType := s.product.extended("Card Type")
		if cardType == "" {
			cardType = s.product.extended("MonsterType")
		}
		productID := s.product.ProductID
		wantFinishes[productID] = printings[productID]
		for _, finish := range printings[productID] {
			suffix, known := finishSuffix[finish]
			if !known {
				log.Fatalf("product %d carries printing %q, not one of the three this identity scheme knows",
					productID, finish)
			}
			entry := map[string]any{
				"id":        fmt.Sprintf("%s_%d%s", strings.ToLower(s.number), productID, suffix),
				"name":      s.baseName,
				"number":    s.number,
				"setCode":   setCodes[s.product.GroupID],
				"rarity":    s.product.extended("Rarity"),
				"attribute": s.product.extended("Attribute"),
				"type":      cardType,
				"finish":    finish,
				"image":     imageURL(s.product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
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
	counted, err := validate(buf.Bytes(), wantFinishes)
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
// and sealed product carrying its identity — for a card that includes the
// rarity it is varied by and the edition its skus price — every id unique
// within its namespace, every referenced set existing, every finish one of
// the three printing names, and every product's entries covering exactly
// the sku printings the catalog lists for it.
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
	}
	cardIDs := map[string]bool{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" || card.Number == "" || card.Rarity == "" ||
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
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		productID := card.ExternalLinks.TcgPlayerId
		if sliceContains(gotFinishes[productID], card.Finish) {
			return out, fmt.Errorf("product %d carries finish %q twice", productID, card.Finish)
		}
		gotFinishes[productID] = append(gotFinishes[productID], card.Finish)
	}
	if len(gotFinishes) != len(wantFinishes) {
		return out, fmt.Errorf("entries cover %d products, catalog carries %d", len(gotFinishes), len(wantFinishes))
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

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
