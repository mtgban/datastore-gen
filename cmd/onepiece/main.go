// Command onepiece builds the One Piece datastore file consumed by
// go-mtgban's mtgmatcher/onepiece loader, from the TCGplayer catalog dump
// for category 68 and the punk-records mirror of Bandai's official card
// list.
//
// Identity is the catalog's, one entry per English product and sku
// printing: TCGplayer prices Normal and Foil as separate sku printings of
// one product, so each printing is its own entry with its own id, priced
// by construction — the single-entry-per-product shape this datastore used
// to publish folded both price points onto one id where a product carries
// both. The id's finish suffix derives from the printing name alone, never
// from which sibling printings exist, so an id cannot churn when TCGplayer
// later adds a printing to a product. The recon census measured only 82%
// of collector numbers aligning between the two sources by count alone —
// Bandai's _pN ordinals are annotation here, attached where the alignment
// is unambiguous, never identity.
//
// The name parentheticals TCGplayer decorates products with fall into three
// kinds, told apart per collector number: a parenthetical every product of
// the number carries is part of the card's name (the "(Bentham)" epithets);
// a number disambiguator ("(003)", "(OP01-003)") is dropped; whatever
// remains is the variant label ("Alternate Art", "Manga", "SP", event
// names) the matcher narrows on.
//
// Japanese-version products are dropped: the datastore is the English
// program. DON!! cards have no collector number to carry — neither the
// catalog nor Bandai's own card list numbers them — so they are filed
// under one constant number and told apart by their set and their variant
// label, which is how the storefronts name them too.
//
// Sealed products are everything the catalog files outside the singles
// type, same as the other games: by exclusion, so a product type TCGplayer
// adds later lands on the sealed side where it is noticed.
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
	"sort"
	"strings"
)

const (
	onepieceCategory = 68

	// tcgSingles is the product type single cards are filed under;
	// everything else is sealed by exclusion.
	tcgSingles = "Cards"

	// donCardType is the card type the catalog gives every DON!! card. The
	// rarity does not answer for them - TCGplayer files the event ones as
	// "PR" alongside the promo characters - but the type does.
	donCardType = "DON!!"

	// donNumber stands in for the collector number a DON!! card does not
	// have; the loader rejects a datastore whose cards carry none, so the
	// stand-in has to be something. A constant is the stable choice: a
	// counted or ordered number would renumber the cards the day TCGplayer
	// adds a product, rewriting the price history keyed by it, and a number
	// derived per product would read as a real one and invite a
	// storefront's stray digits to match it. Identity falls to the set and
	// the variant label instead, which the catalog already spells out per
	// product and which no two DON!! products share.
	donNumber = "DON"

	punkCardsURL = "https://raw.githubusercontent.com/buhbbl/punk-records/main/english/index/cards_by_id.json"
)

// finishSuffix maps each sku printing name to the suffix its entry's id
// carries; Normal is the bare id. Any other printing name is a hard
// failure, because a suffix invented on the fly would not be a stable
// identity.
var finishSuffix = map[string]string{
	"Normal": "",
	"Foil":   "_foil",
}

// finishOrder fixes the order a product's entries are emitted in.
var finishOrder = []string{
	"Normal",
	"Foil",
}

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the distinct printing names its skus
// carry, in finishOrder; a printing the catalog does not list for a product
// is one that does not exist.

// punkCard is the slice of a punk-records printing this build reads: the
// _pN-suffixed card id is Bandai's own printing identity, mirrored from
// the official card list.
type punkCard struct {
	CardID string `json:"card_id"`
	ImgURL string `json:"img_url"`
}

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

// cardImage picks the card's image. Every official image of the game -
// Bandai's own card list and TCGplayer's copy of it alike - wears a giant
// SAMPLE watermark, and the community onepiece.gg mirror keys its cleaned
// renditions by the same Bandai printing id the datastore aligns, so the
// clean image is derivable exactly where the printing identity is known:
// the aligned printings by their id, the base printings by their bare
// number. An unaligned variant keeps the watermarked catalog image, whose
// art is at least the right one, and so does every DON!! card: the number
// they are filed under is this builder's, not a printing id the mirror
// could know.
func cardImage(s single, bandaiId string) string {
	if bandaiId != "" {
		return "https://static.dotgg.gg/onepiece/card/" + bandaiId + ".webp"
	}
	if len(s.quals) == 0 && s.number != donNumber {
		return "https://static.dotgg.gg/onepiece/card/" + s.number + ".webp"
	}
	return imageURL(s.product.ImageURL)
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
var bareNumRe = regexp.MustCompile(`^\d{3}$`)

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []string
}

// decorations strips the collector number worn as decoration: a dash
// suffix ("Yamato - OP16-098") and the parenthetical forms ("(003)",
// "(OP01-003)").
func decompose(p tcgplayer.Product, num string) single {
	name := p.Name
	name = strings.ReplaceAll(name, " - "+num, "")

	var quals []string
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if bareNumRe.MatchString(q) || strings.EqualFold(q, num) {
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
	minCards := flag.Int("min-cards", 6000, "refuse to emit a datastore with fewer card entries")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 68 (required)")
	punkCards := flag.String("punk-cards", punkCardsURL, "punk-records cards_by_id file, path or URL")
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
	if catalog.Category.CategoryID != onepieceCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, onepieceCategory)
	}

	punkData, err := fetch(*punkCards)
	if err != nil {
		log.Fatalln("punk-records:", err)
	}
	var punk map[string]punkCard
	if err := json.Unmarshal(punkData, &punk); err != nil {
		log.Fatalln("punk-records:", err)
	}
	punkByNumber := map[string][]string{}
	for id := range punk {
		base := strings.SplitN(id, "_p", 2)[0]
		punkByNumber[base] = append(punkByNumber[base], id)
	}
	for _, ids := range punkByNumber {
		sort.Strings(ids)
	}
	log.Printf("catalog: %d groups, %d products; punk-records: %d printings over %d numbers",
		len(catalog.Groups), len(catalog.Products), len(punk), len(punkByNumber))

	groupByID := map[int]tcgplayer.Group{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}

	printings := catalog.PrintingNames()

	// Split the products: English singles become printings, numbered or
	// DON!!, the non-single types become sealed, and the rest is counted
	// out loud.
	var singles []single
	var sealedProducts []tcgplayer.Product
	var japanese, unnumbered, donCards, printingless int
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if strings.Contains(strings.ToLower(product.Name), "japanese") {
			japanese++
			continue
		}
		num := product.Extended("Number")
		if num == "" {
			if !strings.EqualFold(product.Extended("CardType"), donCardType) {
				unnumbered++
				continue
			}
			num = donNumber
			donCards++
		}
		if len(printings[product.ProductID]) == 0 {
			printingless++
			log.Printf("no sku printing: %q (%d) left out", product.Name, product.ProductID)
			continue
		}
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept (%d of them DON!!), %d japanese dropped, %d unnumbered and %d printingless left out",
		len(singles), donCards, japanese, unnumbered, printingless)

	// Per collector number: a qualifier every product of the number carries
	// is part of the name (the "(Bentham)" epithets), not a variant. A
	// number with a single product cannot make that call alone, so the
	// epithets learned from the multi-product numbers decide for it — the
	// same epithet decorates the character's every printing. The DON!!
	// bucket is the one holding unrelated cards rather than one card's
	// printings, and it holds undecorated ones, so the rule finds nothing
	// common there and hands every qualifier to the variant label.
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

	// Annotate Bandai's _pN printing id where the two sources align
	// unambiguously: same printing count for the number, base product to
	// the bare id, variant products in product-id order to _p1, _p2, ...
	// A number the sources disagree on is left unannotated, not guessed.
	var annotated int
	bandaiIDs := map[int]string{}
	for num, bucket := range byNumber {
		ids := punkByNumber[num]
		if len(ids) == 0 || len(ids) != len(bucket) {
			continue
		}
		ordered := append([]*single(nil), bucket...)
		sort.Slice(ordered, func(i, j int) bool {
			bi, bj := len(ordered[i].quals) == 0, len(ordered[j].quals) == 0
			if bi != bj {
				return bi
			}
			return ordered[i].product.ProductID < ordered[j].product.ProductID
		})
		for i, s := range ordered {
			bandaiIDs[s.product.ProductID] = ids[i]
		}
		annotated += len(bucket)
	}
	log.Printf("bandai ids: %d of %d printings annotated", annotated, len(singles))

	// Emit. Sets are the catalog groups; ids embed the product id so they
	// survive any upstream renumbering.
	sets := map[string]any{}
	for _, group := range catalog.Groups {
		sets[group.Abbreviation] = map[string]any{
			"name":        group.Name,
			"releaseDate": group.ReleaseDate(),
		}
	}

	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	var cards []any
	wantFinishes := map[int][]string{}
	for _, s := range singles {
		group := groupByID[s.product.GroupID]
		productID := s.product.ProductID
		wantFinishes[productID] = printings[productID]
		for _, finish := range printings[productID] {
			suffix, known := finishSuffix[finish]
			if !known {
				log.Fatalf("product %d carries printing %q, not one of the two this identity scheme knows",
					productID, finish)
			}
			entry := map[string]any{
				"id":      fmt.Sprintf("%s_%d%s", strings.ToLower(s.number), productID, suffix),
				"name":    s.baseName,
				"number":  s.number,
				"setCode": group.Abbreviation,
				"rarity":  s.product.Extended("Rarity"),
				"color":   s.product.Extended("Color"),
				"type":    s.product.Extended("CardType"),
				"finish":  finish,
				"image":   cardImage(s, bandaiIDs[productID]),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
			}
			if bandai, found := bandaiIDs[productID]; found {
				entry["bandaiId"] = bandai
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
			"id":          fmt.Sprintf("%s-%d", strings.ToLower(group.Abbreviation), product.ProductID),
			"name":        product.Name,
			"setCode":     group.Abbreviation,
			"releaseDate": group.ReleaseDate(),
			"image":       imageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	log.Printf("emitting %d sets, %d card entries over %d products, %d sealed",
		len(sets), len(cards), len(singles), len(sealed))

	doc := map[string]any{
		"game":   "onepiece",
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
	// mtgmatcher/onepiece reads, duplicated so this repository depends on
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
// and sealed product carrying its identity, every id unique within its
// namespace, no two entries wearing the same identity, every referenced
// set existing, every finish one of the two printing names, and every
// product's entries covering exactly the sku printings the catalog lists
// for it.
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

	if doc.Game != "onepiece" {
		return out, fmt.Errorf("game is %q, not onepiece", doc.Game)
	}
	for code, set := range doc.Sets {
		if set.Name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
	}
	cardIDs := map[string]bool{}
	// A query resolves a card by its name, number, set and variant label,
	// never by the id, so two products wearing all four alike are one card
	// to every consumer and would alias each other's prices. The key holds
	// the product id rather than a flag so a product's own Normal and Foil
	// entries pass while two different products never do - keying on the
	// finish instead would wave through exactly the pair this is meant to
	// catch, since most DON!! products carry a single finish. This is what
	// holds the DON!! cards' constant number up: the day a set labels two
	// of them alike, the build says so instead of publishing the pair.
	identities := map[string]int{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" || card.Number == "" ||
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
		identity := strings.Join([]string{card.Name, card.Number, card.SetCode, card.Variant}, "|")
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
