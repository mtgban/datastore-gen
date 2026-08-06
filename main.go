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

	// tcgcsv republishes the TCGplayer catalog daily; category 89 is
	// Riftbound.
	tcgcsvGroupsURL   = "https://tcgcsv.com/tcgplayer/89/groups"
	tcgcsvProductsURL = "https://tcgcsv.com/tcgplayer/89/%d/products"
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
	flag.Parse()

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

	var groupsResp struct {
		Results []tcgGroup `json:"results"`
	}
	groupsData, err := fetch(tcgcsvGroupsURL)
	if err != nil {
		log.Fatalln("tcgcsv groups:", err)
	}
	if err := json.Unmarshal(groupsData, &groupsResp); err != nil {
		log.Fatalln("tcgcsv groups:", err)
	}
	// Process in a stable order so unchanged data produces byte-identical
	// output (consumers cache the file by etag).
	sort.Slice(groupsResp.Results, func(i, j int) bool {
		return groupsResp.Results[i].Abbreviation < groupsResp.Results[j].Abbreviation
	})

	for _, group := range groupsResp.Results {
		byNumber := galleryByNumber[group.Abbreviation]
		if !isPromoGroup(group) && byNumber == nil {
			// Neither a promo group nor a set the gallery knows: a set the
			// gallery has not published yet, or storefront-only content.
			log.Printf("%s (%s): not in the gallery, skipped", group.Name, group.Abbreviation)
			continue
		}

		productsData, err := fetch(fmt.Sprintf(tcgcsvProductsURL, group.GroupID))
		if err != nil {
			log.Fatalln(group.Name, ":", err)
		}
		var productsResp struct {
			Results []tcgProduct `json:"results"`
		}
		if err := json.Unmarshal(productsData, &productsResp); err != nil {
			log.Fatalln(group.Name, ":", err)
		}

		// A main set: stamp the gallery printings with the TCGplayer product
		// id resolving to them, keyed by collector number.
		if byNumber != nil {
			var stamped int
			var missed []string
			for _, product := range productsResp.Results {
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
				stamped++
			}
			log.Printf("%s (%s): %d printings stamped, %d unknown to the gallery %v",
				group.Name, group.Abbreviation, stamped, len(missed), missed)
			continue
		}

		var added, maxNum int
		for _, product := range productsResp.Results {
			number := product.extended("Number")
			if number == "" {
				// Not a single (sealed, accessories, the odd unnumbered
				// promo): nothing to identify it by.
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
	var identified int
	for _, uuid := range backend.GetUUIDs() {
		co, err := backend.GetUUID(uuid)
		if err == nil && !strings.HasSuffix(uuid, "_f") && co.Identifiers["tcgplayerProductId"] != "" {
			identified++
		}
	}
	printings := len(cardItems)
	log.Printf("validated: %d sets, %d printings (%d uuids), %d tcgplayer ids",
		len(setItems), printings, len(backend.GetUUIDs()), identified)
	if printings < *minCards {
		log.Fatalf("only %d printings (minimum %d); refusing to publish", printings, *minCards)
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
