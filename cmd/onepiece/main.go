// Command onepiece builds the One Piece datastore file consumed by
// go-mtgban's mtgmatcher/onepiece loader, from the TCGplayer catalog dump
// for category 68 and the punk-records mirror of Bandai's official card
// list.
//
// Identity is the catalog's: every English single product is one printing,
// with an id minted from its collector number and product id, so every
// printing is priced by construction and the id space never depends on a
// cross-source join. The recon census measured only 82% of collector
// numbers aligning between the two sources by count alone — Bandai's _pN
// ordinals are annotation here, attached where the alignment is unambiguous,
// never identity.
//
// The name parentheticals TCGplayer decorates products with fall into three
// kinds, told apart per collector number: a parenthetical every product of
// the number carries is part of the card's name (the "(Bentham)" epithets);
// a number disambiguator ("(003)", "(OP01-003)") is dropped; whatever
// remains is the variant label ("Alternate Art", "Manga", "SP", event
// names) the matcher narrows on.
//
// Japanese-version products are dropped: the datastore is the English
// program. DON!! cards carry no collector number in the catalog and are
// left out of this version; the audit counts them so the gap stays visible.
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

	punkCardsURL = "https://raw.githubusercontent.com/buhbbl/punk-records/main/english/index/cards_by_id.json"
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
	Groups   []tcgGroup   `json:"groups"`
	Products []tcgProduct `json:"products"`
}

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
	product  tcgProduct
	number   string
	baseName string
	quals    []string
}

// decorations strips the collector number worn as decoration: a dash
// suffix ("Yamato - OP16-098") and the parenthetical forms ("(003)",
// "(OP01-003)").
func decompose(p tcgProduct, num string) single {
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
	minCards := flag.Int("min-cards", 5000, "refuse to emit a datastore with fewer card printings")
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
	var catalog tcgCatalog
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

	groupByID := map[int]tcgGroup{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}

	// Split the products: numbered English singles become printings, the
	// non-single types become sealed, and the rest is counted out loud.
	var singles []single
	var sealedProducts []tcgProduct
	var japanese, unnumbered, donCards int
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if strings.Contains(strings.ToLower(product.Name), "japanese") {
			japanese++
			continue
		}
		num := product.extended("Number")
		if num == "" {
			if strings.EqualFold(product.extended("Rarity"), "DON!!") {
				donCards++
			} else {
				unnumbered++
			}
			continue
		}
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept, %d japanese dropped, %d DON!! and %d unnumbered left out",
		len(singles), japanese, donCards, unnumbered)

	// Per collector number: a qualifier every product carries is part of
	// the name, not a variant.
	byNumber := map[string][]*single{}
	for i := range singles {
		byNumber[singles[i].number] = append(byNumber[singles[i].number], &singles[i])
	}
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
		for _, s := range bucket {
			var name, variant []string
			name = append(name, s.baseName)
			for _, q := range s.quals {
				if common[q] == len(bucket) {
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
			"releaseDate": group.releaseDate(),
		}
	}

	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	var cards []any
	for _, s := range singles {
		group := groupByID[s.product.GroupID]
		entry := map[string]any{
			"id":      fmt.Sprintf("%s_%d", strings.ToLower(s.number), s.product.ProductID),
			"name":    s.baseName,
			"number":  s.number,
			"setCode": group.Abbreviation,
			"rarity":  s.product.extended("Rarity"),
			"color":   s.product.extended("Color"),
			"type":    s.product.extended("CardType"),
			"image":   imageURL(s.product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": s.product.ProductID,
			},
		}
		if len(s.quals) > 0 {
			entry["variant"] = strings.Join(s.quals, " ")
		}
		if bandai, found := bandaiIDs[s.product.ProductID]; found {
			entry["bandaiId"] = bandai
		}
		cards = append(cards, entry)
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
	// mtgmatcher/onepiece reads, duplicated so this repository depends on
	// nothing.
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
