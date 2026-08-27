// Command riftbound-datastore builds the Riftbound datastore file consumed
// by go-mtgban's mtgmatcher/riftbound loader: it downloads the official
// card-gallery payload (resolving the current site build id), stamps every
// printing with the TCGplayer product id resolving to it, and appends the
// printings TCGplayer carries but the gallery does not, the promotional
// ones as separate promo-typed sets, so promo listings resolve to their own
// uuids instead of polluting the main printings.
//
// The gallery says which printings are published, never which products
// exist: every product the catalog types as a card becomes a printing, and
// validate refuses a build that left one out. A group the gallery has no
// set for is a set of its own rather than a group to skip — the sealed
// products of such a group were being published while its singles were
// dropped, which is not a position the gallery has any say in. A dual-faced
// token product is adopted like any other printing the gallery does not
// carry: the gallery files one row per face and the catalog sells the card
// once under both names, so the composite number is the printing's own and
// the single-face rows keep theirs. A product the catalog gives no number
// is filed under its product id, which is already the id such a printing
// carries and is no shape a Riftbound collector number takes.
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
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	galleryPageURL = "https://riftbound.leagueoflegends.com/en-us/card-gallery/"
	galleryDataURL = "https://riftbound.leagueoflegends.com/_next/data/%s/en-us/card-gallery.json"

	// riftboundCategory is Riftbound's TCGplayer category, the one the
	// catalog dump is expected to carry.
	riftboundCategory = 89
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game. Everything else the catalog carries is
// a sealed product: the comparison is against the singles types rather
// than a list of sealed ones, so a type TCGplayer adds later lands on the
// sealed side where it is noticed instead of silently passing as a single.
var tcgSingles = tcgplayer.SinglesProductTypes(riftboundCategory)

var buildIdRe = regexp.MustCompile(`"buildId":"([^"]+)"`)

// galleryPayload reads the card-gallery payload: a local file when one is
// named, the live site otherwise, resolving the build id the data URL is
// keyed by.
func galleryPayload(location string) ([]byte, error) {
	if location != "" {
		return os.ReadFile(location)
	}
	page, err := fetch(galleryPageURL)
	if err != nil {
		return nil, err
	}
	m := buildIdRe.FindSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("%s: no buildId in the page", galleryPageURL)
	}
	return fetch(fmt.Sprintf(galleryDataURL, m[1]))
}

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

// numberFor is the collector number a printing is filed under: the
// catalog's own Number, or the product id where the catalog gives none.
// The id is already what such a printing's own id is built from, and six
// digits is no shape a Riftbound collector number takes, so nothing a
// storefront writes can be mistaken for it.
func numberFor(product tcgplayer.Product) string {
	number := product.Extended("Number")
	if number != "" {
		return number
	}
	return strconv.Itoa(product.ProductID)
}

// publicCode spells the code the gallery gives a printing, "<set>-<number>",
// without the spacing a dual-faced number wears around its slash: the loader
// reads a printing's number back off this code by cutting at the slash, and
// "T06 // T04" would leave it a number ending in a space to compare a
// storefront's own against.
func publicCode(group tcgplayer.Group, number string) string {
	return group.Abbreviation + "-" + strings.Join(strings.Fields(number), "")
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
// adoptedCard builds a gallery card entry for a printing only the catalog
// knows about, so a set carries every printing sold under its name rather
// than only those the gallery published.
func adoptedCard(group tcgplayer.Group, product tcgplayer.Product, number string, printings []string) map[string]any {
	name, qualifiers := splitQualifiers(product.Name)
	// A qualifier that only repeats the collector number ("Fury Rune
	// (R01a)") says nothing the number field does not, and would cost the
	// name every storefront actually writes. Any other one is a real
	// distinction between printings and is kept.
	var promoTypes []string
	for _, qualifier := range qualifiers {
		if strings.EqualFold(numberOf(qualifier), numberOf(number)) {
			continue
		}
		promoTypes = append(promoTypes, strings.ToLower(qualifier))
	}

	item := map[string]any{
		"id":                 fmt.Sprintf("%s-%d", strings.ToLower(group.Abbreviation), product.ProductID),
		"name":               name,
		"publicCode":         publicCode(group, number),
		"orientation":        "portrait",
		"tcgplayerProductId": product.ProductID,
		"finishes":           printings,
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
	return item
}

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
// unique across cards and sealed products alike, and every product the
// catalog types as a card carried by a printing — the zero-skip invariant,
// checked on the encoded output so a product no rule above knew what to do
// with stops the publish instead of quietly leaving the datastore. It
// returns the set, printing, sealed and identified-printing counts.
// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func validate(data []byte, cardProducts map[int]bool) (sets, cards, sealed, identified int, err error) {
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
		// A set id is the gallery's own code and every printing names its
		// set by it, so two sets wearing one id are one set to the loader:
		// whichever it indexes last answers for both, and the other's name
		// and release date are gone. The gallery cannot be asked to rename
		// a set, so a collision - two catalog groups sharing an
		// abbreviation - stops the publish rather than being repaired here.
		setIDs := map[string]bool{}
		for _, set := range blade.Sets.Items {
			if set.ID == "" || set.Name == "" || set.ReleaseDate == "" {
				return 0, 0, 0, 0, fmt.Errorf("set %q (%s) missing identity or date", set.Name, set.ID)
			}
			if !codeShape.MatchString(set.ID) {
				return 0, 0, 0, 0, fmt.Errorf("set code %q holds what a query cannot carry", set.ID)
			}
			if setIDs[set.ID] {
				return 0, 0, 0, 0, fmt.Errorf("duplicate set id %s", set.ID)
			}
			setIDs[set.ID] = true
		}
		carried := map[int]bool{}
		for _, card := range blade.Cards.Items {
			if card.ID == "" || card.Name == "" || card.PublicCode == "" {
				return 0, 0, 0, 0, fmt.Errorf("printing %q (%s) missing identity", card.Name, card.ID)
			}
			if ids[card.ID] {
				return 0, 0, 0, 0, fmt.Errorf("duplicate id %s", card.ID)
			}
			ids[card.ID] = true
			if card.TCGplayerProductID == 0 {
				continue
			}
			identified++
			// A product resolves to one printing: two printings claiming
			// it would split its price history between them.
			if carried[card.TCGplayerProductID] {
				return 0, 0, 0, 0, fmt.Errorf("product %d claimed by two printings", card.TCGplayerProductID)
			}
			if !cardProducts[card.TCGplayerProductID] {
				return 0, 0, 0, 0, fmt.Errorf("printing %q (%s) names product %d, which the catalog does not type as a card",
					card.Name, card.ID, card.TCGplayerProductID)
			}
			carried[card.TCGplayerProductID] = true
		}
		var missing []int
		for productID := range cardProducts {
			if !carried[productID] {
				missing = append(missing, productID)
			}
		}
		sort.Ints(missing)
		if len(missing) > 0 {
			return 0, 0, 0, 0, fmt.Errorf("%d catalog card products carry no printing, first is %d",
				len(missing), missing[0])
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

// datastoreCounts is what a datastore holds: the two totals, and the printing
// count per set. It is read off an encoded datastore - this build's own, or
// the one it is about to replace - so both sides are counted the same way
// by the same code.
type datastoreCounts struct {
	cards, sealed int
	bySet         map[string]int
}

func countDatastore(data []byte) (datastoreCounts, error) {
	var doc struct {
		PageProps struct {
			Page struct {
				Blades []struct {
					Type  string `json:"type"`
					Cards struct {
						Items []struct {
							Set struct {
								Value struct {
									ID string `json:"id"`
								} `json:"value"`
							} `json:"set"`
						} `json:"items"`
					} `json:"cards"`
					Sealed struct {
						Items []json.RawMessage `json:"items"`
					} `json:"sealed"`
				} `json:"blades"`
			} `json:"page"`
		} `json:"pageProps"`
	}
	out := datastoreCounts{bySet: map[string]int{}}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	for _, blade := range doc.PageProps.Page.Blades {
		if blade.Type != "riftboundCardGallery" {
			continue
		}
		out.cards = len(blade.Cards.Items)
		out.sealed = len(blade.Sealed.Items)
		for _, card := range blade.Cards.Items {
			out.bySet[card.Set.Value.ID]++
		}
		return out, nil
	}
	return out, fmt.Errorf("no card gallery blade")
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
	minCards := flag.Int("min-cards", 1000, "refuse to emit a datastore with fewer card printings")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 89 (required)")
	galleryPath := flag.String("gallery", "", "card-gallery payload file (default: fetch the live gallery)")
	against := flag.String("against", "", "previous datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.02, "the share of its cards or sealed products a build may lose")
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
	// The coverage contract: every product the catalog types as a card.
	// validate reads it back off the encoded output, so a product no rule
	// here carried fails the build instead of leaving the datastore.
	cardProducts := map[int]bool{}
	for _, product := range catalog.Products {
		productsByGroup[product.GroupID] = append(productsByGroup[product.GroupID], product)
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
	for _, products := range productsByGroup {
		sort.Slice(products, func(i, j int) bool {
			return products[i].ProductID < products[j].ProductID
		})
	}
	log.Printf("catalog: %d groups, %d products (%d singles), %d with a known finish",
		len(catalog.Groups), len(catalog.Products), singles, len(finishes))

	payload, err := galleryPayload(*galleryPath)
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
		products := productsByGroup[group.GroupID]

		// A set the gallery published: stamp its printings with the
		// TCGplayer product id resolving to them, keyed by collector
		// number, and adopt the products it does not carry.
		if byNumber != nil {
			if item := setByID[group.Abbreviation]; item != nil {
				item["releaseDate"] = group.ReleaseDate()
			}
			// One gallery row holds one product id, so a second product
			// landing on a number already stamped is adopted rather than
			// overwriting the first and losing itself.
			stampedBy := map[string]int{}
			var stamped, adopted int
			for _, product := range products {
				if !slices.Contains(tcgSingles, product.ProductType) {
					continue
				}
				number := numberFor(product)
				key := numberOf(number)
				item, found := byNumber[key]
				if !found || stampedBy[key] != 0 {
					// A printing TCGplayer carries and the gallery does
					// not - the rune variants above all, which storefronts
					// sell by the hundred, and the dual-faced tokens the
					// gallery files one row per face of while the catalog
					// sells the card once under both names. Adopt it into
					// the set on the catalog's word, the same terms the
					// promo groups are carried on.
					cardItems = append(cardItems, adoptedCard(group, product, number, finishes[product.ProductID]))
					adopted++
					continue
				}
				stampedBy[key] = product.ProductID
				item["tcgplayerProductId"] = product.ProductID
				if f := finishes[product.ProductID]; len(f) > 0 {
					item["finishes"] = f
				}
				stamped++
			}
			log.Printf("%s (%s): %d printings stamped, %d adopted",
				group.Name, group.Abbreviation, stamped, adopted)
			continue
		}

		// A group the gallery has no set for: the promotional ones, and a
		// set sold before the gallery published it. Its printings are the
		// catalog's alone, so they are minted here and the set with them.
		var added, maxNum int
		for _, product := range products {
			if !slices.Contains(tcgSingles, product.ProductType) {
				continue
			}
			number := numberFor(product)
			collector := 0
			fmt.Sscanf(strings.TrimLeft(product.Extended("Number"), "0"), "%d", &collector)
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
				"publicCode":         publicCode(group, number),
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

		set := map[string]any{
			"id":                 group.Abbreviation,
			"name":               group.Name,
			"collectorNumberMax": maxNum,
			"releaseDate":        group.ReleaseDate(),
		}
		// The promo type gates how a printing matches, so only the groups
		// that hold promotional printings carry it: a set the gallery has
		// merely not published yet is a main set, whatever it is missing.
		if isPromoGroup(group) {
			set["type"] = "promo"
		}
		setItems = append(setItems, set)
		log.Printf("%s (%s): %d printings minted with a set of their own",
			group.Name, group.Abbreviation, added)
	}

	// Sealed products: everything the catalog files outside the singles
	// type, from every group whether the gallery knows it or not - the
	// gallery carries no product entity of any kind, so it has no say.
	var sealedItems []any
	for _, group := range groups {
		for _, product := range productsByGroup[group.GroupID] {
			if slices.Contains(tcgSingles, product.ProductType) {
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
	sets2, cards2, sealed2, identified, err := validate(buf.Bytes(), cardProducts)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d printings, %d tcgplayer ids, %d sealed",
		sets2, cards2, identified, sealed2)
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		identified, singles, singles-identified)
	if sets2 != len(setItems) || cards2 != len(cardItems) || sealed2 != len(sealedItems) {
		log.Fatalf("emitted %d sets, %d printings, %d sealed but read back %d, %d, %d; refusing to publish",
			len(setItems), len(cardItems), len(sealedItems), sets2, cards2, sealed2)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a card, so it is exhaustive by construction
	// and cannot lose a product to a rule that did not know what to do with
	// it - the printing side's whole failure mode. What it can lose a
	// product to is an edit: one `continue` on the sealed path and the
	// products would leave the datastore with nothing to say so, the
	// printing side's invariant being blind to them. Counting the emitted
	// products back against the catalog total is what says so.
	wantSealed := len(catalog.Products) - singles
	if sealed2 != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			sealed2, wantSealed)
	}
	if cards2 < *minCards {
		log.Fatalf("only %d printings (minimum %d); refusing to publish", cards2, *minCards)
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
		log.Printf("against %s: %d printings (was %d), %d sealed (was %d), %d sets (was %d)",
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
