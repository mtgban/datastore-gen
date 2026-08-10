// Command riftbound-datastore builds the Riftbound datastore file consumed
// by go-mtgban's mtgmatcher/riftbound loader: it downloads the official
// card-gallery payload (resolving the current site build id), stamps every
// printing with the TCGplayer product id resolving to it, and appends the
// promotional printings TCGplayer carries but the gallery does not, as
// separate promo-typed sets, so promo listings resolve to their own uuids
// instead of polluting the main printings.
//
// The output is the gallery payload itself with the extra data merged into
// the gallery blade, so mtgmatcher/riftbound loads it unchanged — and it is
// round-tripped through that very loader before being written, so a broken
// upstream payload can never be published.
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

	"github.com/mtgban/go-mtgban/mtgmatcher/riftbound"
)

const (
	galleryPageURL = "https://riftbound.leagueoflegends.com/en-us/card-gallery/"
	galleryDataURL = "https://riftbound.leagueoflegends.com/_next/data/%s/en-us/card-gallery.json"

	// riftboundCategory is Riftbound's TCGplayer category, the one the
	// catalog dump is expected to carry.
	riftboundCategory = 89

	// tcgSingles is the product type single cards are filed under.
	// Everything else the catalog carries is a sealed product: the
	// comparison is against the singles type rather than a list of sealed
	// ones, so a type TCGplayer adds later lands on the sealed side where
	// it is noticed instead of silently passing as a single.
	tcgSingles = "Cards"
)

var buildIdRe = regexp.MustCompile(`"buildId":"([^"]+)"`)

func fetch(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type tcgGroup struct {
	GroupID      int    `json:"groupId"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
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
	// Skus enumerate every printing/condition/language a product is sold
	// in. Only the printing matters here, and it is the whole reason the
	// catalog dump is read instead of a price feed: a printing exists
	// whether or not anyone happens to be selling it today.
	Skus []struct {
		PrintingID int `json:"printingId"`
	} `json:"skus"`
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

// finishesByProduct maps each product to the finishes it is sold in, named as
// the matcher names them. TCGplayer calls them Normal and Foil, and a
// printing it does not list is one that does not exist: most of Riftbound is
// sold in a single finish, promotional printings being foil and starter
// cards plain.
func (c *tcgCatalog) finishesByProduct() map[int][]string {
	printing := map[int]string{}
	for _, p := range c.Printings {
		switch p.Name {
		case "Normal":
			printing[p.PrintingID] = "nonfoil"
		case "Foil":
			printing[p.PrintingID] = "foil"
		}
	}

	out := map[int][]string{}
	for _, product := range c.Products {
		var nonfoil, foil bool
		for _, sku := range product.Skus {
			switch printing[sku.PrintingID] {
			case "nonfoil":
				nonfoil = true
			case "foil":
				foil = true
			}
		}
		// Ordered rather than as encountered, so unchanged data keeps
		// producing byte-identical output.
		var finishes []string
		if nonfoil {
			finishes = append(finishes, "nonfoil")
		}
		if foil {
			finishes = append(finishes, "foil")
		}
		if len(finishes) > 0 {
			out[product.ProductID] = finishes
		}
	}
	return out
}

func (p tcgProduct) extended(name string) string {
	for _, e := range p.ExtendedData {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

// isPromoGroup reports whether a TCGplayer group holds promotional printings
// rather than a main set the gallery already covers (or will cover once
// published, like preview-season sets).
func isPromoGroup(g tcgGroup) bool {
	return strings.Contains(g.Name, "Promotional") || strings.Contains(g.Name, "Bundle")
}

// numberOf reduces a collector number or public code to the loader's
// canonical form: what follows any set prefix, without the "/total" tail.
func numberOf(code string) string {
	if idx := strings.IndexByte(code, '-'); idx >= 0 {
		code = code[idx+1:]
	}
	code = strings.Split(code, "/")[0]
	return strings.ToLower(riftbound.CanonicalNumber(code))
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 1000, "refuse to emit a datastore with fewer card printings")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 89 (required)")
	flag.Parse()

	if *catalogPath == "" {
		log.Fatalln("-tcg-catalog is required: the dump carries the product ids and the finishes")
	}
	catalogData, err := os.ReadFile(*catalogPath)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	var catalog tcgCatalog
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != riftboundCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, riftboundCategory)
	}
	finishes := catalog.finishesByProduct()
	productsByGroup := map[int][]tcgProduct{}
	var singles int
	for _, product := range catalog.Products {
		productsByGroup[product.GroupID] = append(productsByGroup[product.GroupID], product)
		if product.ProductType == tcgSingles {
			singles++
		}
	}
	// A dump from before the product type was recorded types nothing, and
	// the sealed-by-exclusion rule would then file the whole catalog as
	// sealed; a dump whose singles all vanished is equally implausible.
	if singles == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}
	for _, products := range productsByGroup {
		sort.Slice(products, func(i, j int) bool {
			return products[i].ProductID < products[j].ProductID
		})
	}
	log.Printf("catalog: %d groups, %d products (%d singles), %d with a known finish",
		len(catalog.Groups), len(catalog.Products), singles, len(finishes))

	page, err := fetch(galleryPageURL)
	if err != nil {
		log.Fatalln("gallery page:", err)
	}
	m := buildIdRe.FindSubmatch(page)
	if m == nil {
		log.Fatalln("no buildId found in the gallery page")
	}
	payload, err := fetch(fmt.Sprintf(galleryDataURL, m[1]))
	if err != nil {
		log.Fatalln("gallery payload:", err)
	}

	// Decode the payload generically so everything the loader does not care
	// about survives the round trip untouched.
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		log.Fatalln("gallery payload:", err)
	}
	blades, _ := doc["pageProps"].(map[string]any)["page"].(map[string]any)["blades"].([]any)
	var gallery map[string]any
	for _, b := range blades {
		blade, ok := b.(map[string]any)
		if ok && blade["type"] == "riftboundCardGallery" {
			gallery = blade
			break
		}
	}
	if gallery == nil {
		log.Fatalln("no card gallery blade in the payload")
	}
	sets := gallery["sets"].(map[string]any)
	cards := gallery["cards"].(map[string]any)
	setItems := sets["items"].([]any)
	cardItems := cards["items"].([]any)

	// Index the gallery printings by set and canonical collector number, the
	// identity TCGplayer products are mapped back onto.
	galleryByNumber := map[string]map[string]map[string]any{}
	for _, c := range cardItems {
		item := c.(map[string]any)
		setID := item["set"].(map[string]any)["value"].(map[string]any)["id"].(string)
		if galleryByNumber[setID] == nil {
			galleryByNumber[setID] = map[string]map[string]any{}
		}
		galleryByNumber[setID][numberOf(item["publicCode"].(string))] = item
	}

	// Process in a stable order so unchanged data produces byte-identical
	// output (consumers cache the file by etag).
	groups := catalog.Groups
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Abbreviation < groups[j].Abbreviation
	})

	for _, group := range groups {
		byNumber := galleryByNumber[group.Abbreviation]
		if !isPromoGroup(group) && byNumber == nil {
			// Neither a promo group nor a set the gallery knows: a set the
			// gallery has not published yet, or storefront-only content.
			log.Printf("%s (%s): not in the gallery, skipped", group.Name, group.Abbreviation)
			continue
		}

		products := productsByGroup[group.GroupID]

		// A main set: stamp the gallery printings with the TCGplayer product
		// id resolving to them, keyed by collector number.
		if byNumber != nil {
			var stamped int
			var missed []string
			for _, product := range products {
				if product.ProductType != tcgSingles {
					continue
				}
				number := product.extended("Number")
				if number == "" {
					continue
				}
				item, found := byNumber[numberOf(number)]
				if !found {
					// Printings TCGplayer carries but the gallery does not
					// (rune variants, dual-faced tokens).
					missed = append(missed, fmt.Sprintf("%s %q", number, product.Name))
					continue
				}
				item["tcgplayerProductId"] = product.ProductID
				if f := finishes[product.ProductID]; len(f) > 0 {
					item["finishes"] = f
				}
				stamped++
			}
			log.Printf("%s (%s): %d printings stamped, %d unknown to the gallery %v",
				group.Name, group.Abbreviation, stamped, len(missed), missed)
			continue
		}

		var added, maxNum int
		for _, product := range products {
			if product.ProductType != tcgSingles {
				continue
			}
			number := product.extended("Number")
			if number == "" {
				// An unnumbered single (the odd promo variant): nothing
				// to identify it by.
				continue
			}
			collector := 0
			fmt.Sscanf(strings.TrimLeft(number, "0"), "%d", &collector)
			if collector > maxNum {
				maxNum = collector
			}

			cardItems = append(cardItems, map[string]any{
				// The TCGplayer product id is the stable identity of a
				// promo printing; group-prefixed for readability.
				"id":                 fmt.Sprintf("%s-%d", strings.ToLower(group.Abbreviation), product.ProductID),
				"collectorNumber":    collector,
				"name":               product.Name,
				"publicCode":         fmt.Sprintf("%s-%s", group.Abbreviation, number),
				"orientation":        "portrait",
				"tcgplayerProductId": product.ProductID,
				"finishes":           finishes[product.ProductID],
				"set": map[string]any{
					"value": map[string]any{
						"id":    group.Abbreviation,
						"label": group.Name,
					},
				},
				"rarity": map[string]any{
					"value": map[string]any{
						"id": strings.ToLower(product.extended("Rarity")),
					},
				},
				"cardImage": map[string]any{
					"url": product.ImageURL,
				},
			})
			added++
		}
		if added == 0 {
			continue
		}

		setItems = append(setItems, map[string]any{
			"id":                 group.Abbreviation,
			"name":               group.Name,
			"collectorNumberMax": maxNum,
			"type":               "promo",
		})
		log.Printf("%s (%s): %d promo printings", group.Name, group.Abbreviation, added)
	}

	// Sealed products: everything the catalog files outside the singles
	// type, from every group whether the gallery knows it or not - the
	// gallery carries no product entity of any kind, so it has no say.
	var sealedItems []any
	for _, group := range groups {
		for _, product := range productsByGroup[group.GroupID] {
			if product.ProductType == tcgSingles {
				continue
			}
			sealedItems = append(sealedItems, map[string]any{
				"id":                 fmt.Sprintf("%s-%d", strings.ToLower(group.Abbreviation), product.ProductID),
				"name":               product.Name,
				"tcgplayerProductId": product.ProductID,
				"set": map[string]any{
					"value": map[string]any{
						"id":    group.Abbreviation,
						"label": group.Name,
					},
				},
				"cardImage": map[string]any{
					"url": product.ImageURL,
				},
			})
		}
	}
	if len(sealedItems) > 0 {
		gallery["sealed"] = map[string]any{"items": sealedItems}
	}
	log.Printf("sealed: %d products", len(sealedItems))

	sets["items"] = setItems
	cards["items"] = cardItems

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(doc); err != nil {
		log.Fatalln(err)
	}

	// Round-trip through the real loader before publishing anything: an
	// upstream page redesign or a truncated download must fail here, not in
	// every consumer.
	backend, err := riftbound.Load(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatalln("validation:", err)
	}
	// Count identified printings over the cards themselves: uuids carry
	// one entry per finish, so counting them would tally a printing once
	// per finish it is sold in.
	var identified int
	for _, set := range backend.Sets {
		for _, card := range set.Cards {
			if card.Identifiers["tcgplayerProductId"] != "" {
				identified++
			}
		}
	}
	printings := len(cardItems)
	log.Printf("validated: %d sets, %d printings (%d uuids), %d tcgplayer ids, %d sealed",
		len(setItems), printings, len(backend.GetUUIDs()), identified, len(backend.AllSealedUUIDs))
	if printings < *minCards {
		log.Fatalf("only %d printings (minimum %d); refusing to publish", printings, *minCards)
	}
	if len(backend.AllSealedUUIDs) != len(sealedItems) {
		log.Fatalf("%d sealed products emitted but %d loaded back; refusing to publish",
			len(sealedItems), len(backend.AllSealedUUIDs))
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
