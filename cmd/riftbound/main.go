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
// re-read and structurally verified before being written, so a broken
// upstream payload can never be published.
//
// This repository is deliberately standalone: it produces JSON and depends
// on nothing, so a datastore change never waits on a go-mtgban tag. The
// few helpers the loader also has are duplicated here instead of imported.
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

// releaseDate reduces a group's publishedOn timestamp to the bare day the
// loader parses ("2025-10-31T00:00:00" -> "2025-10-31").

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// finishesByProduct maps each product to the finishes it is sold in, named as
// the matcher names them. TCGplayer calls them Normal and Foil, and a
// printing it does not list is one that does not exist: most of Riftbound is
// sold in a single finish, promotional printings being foil and starter
// cards plain.
func finishesByProduct(c *tcgplayer.CatalogDump) map[int][]string {
	printing := map[int]string{}
	for _, p := range c.Printings {
		switch p.Name {
		case "Normal":
			printing[p.PrintingID] = "nonfoil"
		case "Foil":
			printing[p.PrintingID] = "foil"
		default:
			// Normal and Foil are the category's whole vocabulary today;
			// a printing TCGplayer adds later must be mapped here, not
			// silently skipped (its skus would count for no finish and
			// the loader would fall back to both).
			log.Printf("unknown printing %q (%d): skus under it are ignored", p.Name, p.PrintingID)
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

// isPromoGroup reports whether a TCGplayer group holds promotional printings
// rather than a main set the gallery already covers (or will cover once
// published, like preview-season sets).
func isPromoGroup(g tcgplayer.Group) bool {
	return strings.Contains(g.Name, "Promotional") || strings.Contains(g.Name, "Bundle")
}

// numberOf reduces a collector number or public code to the loader's
// canonical form: what follows any set prefix, without the "/total" tail.
func numberOf(code string) string {
	if idx := strings.IndexByte(code, '-'); idx >= 0 {
		code = code[idx+1:]
	}
	code = strings.Split(code, "/")[0]
	return strings.ToLower(canonicalNumber(code))
}

// canonicalNumber strips leading zeros from the digit run of a collector
// number, preserving any letter prefix ("T01" -> "T1") and any suffix
// ("066a" -> "66a"), duplicating the loader's CanonicalNumber so this
// repository depends on nothing.
func canonicalNumber(number string) string {
	i := 0
	for i < len(number) && (number[i] < '0' || number[i] > '9') {
		i++
	}
	prefix, rest := number[:i], number[i:]
	trimmed := strings.TrimLeft(rest, "0")
	if trimmed == "" && rest != "" {
		trimmed = "0"
	}
	return prefix + trimmed
}

// splitQualifiers splits the trailing parenthetical qualifiers off a
// product name: "Sett - The Boss (Metal) (Best Of)" yields the base name
// and the qualifiers in order. A name that is nothing but a parenthetical
// stays whole.
func splitQualifiers(name string) (string, []string) {
	base := strings.TrimSpace(name)
	var qualifiers []string
	for strings.HasSuffix(base, ")") {
		idx := strings.LastIndexByte(base, '(')
		if idx <= 0 {
			break
		}
		qualifiers = append([]string{strings.TrimSpace(base[idx+1 : len(base)-1])}, qualifiers...)
		base = strings.TrimSpace(base[:idx])
	}
	if base == "" {
		return strings.TrimSpace(name), nil
	}
	return base, qualifiers
}

// validate decodes an encoded datastore and checks its shape: the gallery
// blade present, every set and printing carrying its identity, every id
// unique across cards and sealed products alike. It returns the set,
// printing, sealed and identified-printing counts.
func validate(data []byte) (sets, cards, sealed, identified int, err error) {
	var doc struct {
		PageProps struct {
			Page struct {
				Blades []struct {
					Type string `json:"type"`
					Sets struct {
						Items []struct {
							ID          string `json:"id"`
							Name        string `json:"name"`
							ReleaseDate string `json:"releaseDate"`
						} `json:"items"`
					} `json:"sets"`
					Cards struct {
						Items []struct {
							ID                 string `json:"id"`
							Name               string `json:"name"`
							PublicCode         string `json:"publicCode"`
							TCGplayerProductID int    `json:"tcgplayerProductId"`
						} `json:"items"`
					} `json:"cards"`
					Sealed struct {
						Items []struct {
							ID                 string `json:"id"`
							Name               string `json:"name"`
							TCGplayerProductID int    `json:"tcgplayerProductId"`
						} `json:"items"`
					} `json:"sealed"`
				} `json:"blades"`
			} `json:"page"`
		} `json:"pageProps"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, 0, 0, 0, err
	}

	for _, blade := range doc.PageProps.Page.Blades {
		if blade.Type != "riftboundCardGallery" {
			continue
		}
		ids := map[string]bool{}
		for _, set := range blade.Sets.Items {
			if set.ID == "" || set.Name == "" || set.ReleaseDate == "" {
				return 0, 0, 0, 0, fmt.Errorf("set %q (%s) missing identity or date", set.Name, set.ID)
			}
		}
		for _, card := range blade.Cards.Items {
			if card.ID == "" || card.Name == "" || card.PublicCode == "" {
				return 0, 0, 0, 0, fmt.Errorf("printing %q (%s) missing identity", card.Name, card.ID)
			}
			if ids[card.ID] {
				return 0, 0, 0, 0, fmt.Errorf("duplicate id %s", card.ID)
			}
			ids[card.ID] = true
			if card.TCGplayerProductID != 0 {
				identified++
			}
		}
		for _, product := range blade.Sealed.Items {
			if product.ID == "" || product.Name == "" || product.TCGplayerProductID == 0 {
				return 0, 0, 0, 0, fmt.Errorf("sealed %q (%s) missing identity", product.Name, product.ID)
			}
			if ids[product.ID] {
				return 0, 0, 0, 0, fmt.Errorf("duplicate id %s", product.ID)
			}
			ids[product.ID] = true
		}
		return len(blade.Sets.Items), len(blade.Cards.Items), len(blade.Sealed.Items), identified, nil
	}
	return 0, 0, 0, 0, fmt.Errorf("no card gallery blade in the output")
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
	var catalog tcgplayer.CatalogDump
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != riftboundCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, riftboundCategory)
	}
	finishes := finishesByProduct(&catalog)
	productsByGroup := map[int][]tcgplayer.Product{}
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

	// Index the gallery sets so the groups can stamp their release dates
	setByID := map[string]map[string]any{}
	for _, s := range setItems {
		item, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["id"].(string)
		setByID[id] = item
	}

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
			if item := setByID[group.Abbreviation]; item != nil {
				item["releaseDate"] = group.ReleaseDate()
			}
			var stamped int
			var missed []string
			for _, product := range products {
				if product.ProductType != tcgSingles {
					continue
				}
				number := product.Extended("Number")
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
			number := product.Extended("Number")
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

			// The parenthetical qualifiers become promo types, so sibling
			// promos share one clean name and are told apart by number or
			// by the storefront's own wording matching the types.
			name, qualifiers := splitQualifiers(product.Name)
			var promoTypes []string
			for _, qualifier := range qualifiers {
				promoTypes = append(promoTypes, strings.ToLower(qualifier))
			}

			item := map[string]any{
				// The TCGplayer product id is the stable identity of a
				// promo printing; group-prefixed for readability.
				"id":                 fmt.Sprintf("%s-%d", strings.ToLower(group.Abbreviation), product.ProductID),
				"collectorNumber":    collector,
				"name":               name,
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
						"id": strings.ToLower(product.Extended("Rarity")),
					},
				},
				"cardImage": map[string]any{
					"url": imageURL(product.ImageURL),
				},
			}
			if len(promoTypes) > 0 {
				item["promoTypes"] = promoTypes
			}
			cardItems = append(cardItems, item)
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
			"releaseDate":        group.ReleaseDate(),
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
				"releaseDate":        group.ReleaseDate(),
				"set": map[string]any{
					"value": map[string]any{
						"id":    group.Abbreviation,
						"label": group.Name,
					},
				},
				"cardImage": map[string]any{
					"url": imageURL(product.ImageURL),
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

	// Re-read the encoded output and verify it structurally before
	// publishing anything: an upstream page redesign or a truncated
	// download must fail here, not in every consumer. The types mirror
	// what go-mtgban's mtgmatcher/riftbound reads, duplicated so this
	// repository depends on nothing.
	sets2, cards2, sealed2, identified, err := validate(buf.Bytes())
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d printings, %d tcgplayer ids, %d sealed",
		sets2, cards2, identified, sealed2)
	if sets2 != len(setItems) || cards2 != len(cardItems) || sealed2 != len(sealedItems) {
		log.Fatalf("emitted %d sets, %d printings, %d sealed but read back %d, %d, %d; refusing to publish",
			len(setItems), len(cardItems), len(sealedItems), sets2, cards2, sealed2)
	}
	if cards2 < *minCards {
		log.Fatalf("only %d printings (minimum %d); refusing to publish", cards2, *minCards)
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
