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
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mtgban/datastore-gen/internal/baseline"
	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/go-tcgplayer"
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

var buildIDRe = regexp.MustCompile(`"buildId":"([^"]+)"`)

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
	m := buildIDRe.FindSubmatch(page)
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

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// stringsOf reads a list of strings back off a decoded document: this build
// carries the gallery payload as generic JSON, so a slice it wrote itself
// comes back as []any.
func stringsOf(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		var out []string
		for _, item := range list {
			if name, ok := item.(string); ok {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

// finishesByProduct maps each product to the finishes it is sold in, named
// as TCGplayer names them - which is how the other six games name theirs,
// and what lets a consumer read the datastore's word back against a sku.
// Normal and Foil are this category's whole vocabulary today, and a printing
// TCGplayer adds later is carried under its own name rather than dropped:
// the loader places an unrecognized name by normalizing it, so a third
// finish arrives as data rather than as a release of either side. A printing
// TCGplayer does not list is one that does not exist: most of Riftbound is
// sold in a single finish, promotional printings being foil and starter
// cards plain.
func finishesByProduct(c *tcgplayer.CatalogDump) map[int][]string {
	printing := map[int]string{}
	// The catalog's own order for the printings, which puts Normal before
	// Foil. Ordered rather than as encountered, so unchanged data keeps
	// producing byte-identical output.
	rank := map[string]int{}
	for _, p := range c.Printings {
		printing[p.PrintingID] = p.Name
		rank[p.Name] = p.PrintingID
	}

	out := map[int][]string{}
	for _, product := range c.Products {
		var finishes []string
		for _, sku := range product.Skus {
			name := printing[sku.PrintingID]
			if name == "" || slices.Contains(finishes, name) {
				continue
			}
			finishes = append(finishes, name)
		}
		sort.Slice(finishes, func(i, j int) bool {
			return rank[finishes[i]] < rank[finishes[j]]
		})
		if len(finishes) > 0 {
			out[product.ProductID] = finishes
		}
	}
	return out
}

// catalogFinishes is the category's plain and foil printings, as the dump
// names them. The pair is what a product the catalog prices nothing for
// falls back to, and the names are the catalog's to choose: written here
// they would be a second opinion about somebody else's data, and the wrong
// one the day TCGplayer renames a printing.
func catalogFinishes(c *tcgplayer.CatalogDump) []string {
	seen := map[string]string{}
	for _, printing := range c.Printings {
		switch finish := canonicalFinish(printing.Name); finish {
		case "nonfoil", "foil":
			if _, found := seen[finish]; !found {
				seen[finish] = printing.Name
			}
		}
	}
	var out []string
	for _, finish := range []string{"nonfoil", "foil"} {
		if name := seen[finish]; name != "" {
			out = append(out, name)
		}
	}
	return out
}

// canonicalFinish spells a finish the way the matcher spells it, which is
// what a uuid carries. The datastore names a finish the way TCGplayer prices
// it; the uuids were spelled this way before it did, and a uuid that moves
// resolves to nothing rather than erroring, so the two vocabularies are kept
// apart here rather than merged. It has to agree with go-mtgban's
// CanonicalFinish exactly, the way this repository duplicates every helper
// it shares rather than depending on it.
func canonicalFinish(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	folded := out.String()
	// "Normal" is what TCGplayer calls a plain printing in every category
	if folded == "normal" {
		return "nonfoil"
	}
	return folded
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

// numberWritten is the collector number as a storefront writes it: what
// follows the set prefix, without the "/total" tail, in the case the
// gallery printed it. numberOf beside it folds the same string for
// comparison, lowercasing and stripping leading zeros; this one is the
// value the datastore publishes, because every other game here publishes
// the number as written.
func numberWritten(code string) string {
	if idx := strings.IndexByte(code, '-'); idx >= 0 {
		code = code[idx+1:]
	}
	return printedTotal.ReplaceAllString(strings.Join(strings.Fields(code), ""), "")
}

// printedTotal is the set size a public code prints after the number,
// "227/298", which is not part of the number. A fused token's second face,
// "T02//T04", is: the number is written as both faces and a card named
// "Bird // Buff" is not the T02 the gallery lists alone.
var printedTotal = regexp.MustCompile(`/\d+$`)

// numberOf reduces a collector number or public code to the loader's
// canonical form: what follows any set prefix, without the "/total" tail.
func numberOf(code string) string {
	return strings.ToLower(canonicalNumber(numberWritten(code)))
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
	qualifiers = unnamedQualifiers(qualifiers, name)
	kept := keptQualifiers(qualifiers, number)
	promoTypes := promoTypesOf(qualifiers, number)

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
			"url": emit.ImageURL(product.ImageURL),
		},
	}
	if len(promoTypes) > 0 {
		item["promoTypes"] = promoTypes
		item["variant"] = strings.Join(kept, " ")
	}
	return item
}

// keptQualifiers is the qualifiers that say something about a printing, in
// the spelling the catalog wrote them. Both kinds of printing read it - the
// ones adopted into a gallery set and the ones minted into a set of their
// own - because a qualifier means the same thing either way, and a rule
// written on one path alone reaches half the cards: the six runes the
// promotional set carries were labelled "r01c" through "r06c" while their
// siblings on the other path were not.
//
// A qualifier that only repeats the collector number ("Fury Rune (R01c)"
// filed at R01c) says nothing the number field does not, and would cost the
// name every storefront actually writes. A qualifier naming another set
// ("Body Rune (Vendetta)", filed at R04b in the promotional set) is not
// that: it says which release the rune was printed for, and stays.
//
// "Promo" on the end of a label says what the set the printing is filed
// under already says.
func keptQualifiers(qualifiers []string, number string) []string {
	out := make([]string, 0, len(qualifiers))
	for _, qualifier := range qualifiers {
		if strings.EqualFold(numberOf(qualifier), numberOf(number)) {
			continue
		}
		qualifier = strings.Join(strings.Fields(qualifier), " ")
		if trimmed := strings.TrimSuffix(qualifier, " Promo"); trimmed != "" {
			qualifier = trimmed
		}
		if qualifier == "" || slices.ContainsFunc(out, func(s string) bool {
			return strings.EqualFold(s, qualifier)
		}) {
			continue
		}
		out = append(out, qualifier)
	}
	return out
}

// unnamedQualifiers drops the qualifiers the gallery's own name already
// carries. Riot names the starter-deck printings "Lady of Luminosity -
// Starter" and the catalog writes "Lux, Lady of Luminosity (Starter)" beside
// them, so the word is the card's name rather than a promotion of it -
// stamping it would put "Starter" in a variant as well, and a storefront
// that named it once would have to name it twice to be found.
func unnamedQualifiers(qualifiers []string, name string) []string {
	folded := emit.PromoSlug(name)
	out := make([]string, 0, len(qualifiers))
	for _, qualifier := range qualifiers {
		if slug := emit.PromoSlug(qualifier); slug != "" && strings.Contains(folded, slug) {
			continue
		}
		out = append(out, qualifier)
	}
	return out
}

// promoTypesOf is those labels lowercased, the way every datastore here
// spells a promo type.
func promoTypesOf(qualifiers []string, number string) []string {
	kept := keptQualifiers(qualifiers, number)
	out := make([]string, 0, len(kept))
	for _, qualifier := range kept {
		if slug := emit.PromoSlug(qualifier); slug != "" && !slices.Contains(out, slug) {
			out = append(out, slug)
		}
	}
	return out
}

// printingUUID is the uuid a printing is quoted by: the card's id with the
// finish spelled onto it, and the bare id for the plain printing.
//
// Every other datastore here leaves the plain printing unsuffixed - pokemon's
// "100-102_42348", lorcana's "1" against its "1_foil" - and this one alone
// wrote "_nonfoil" out. A reader holding eight games should not have to know
// which one it is looking at to know what a plain printing is called.
func printingUUID(id, finish string) string {
	canonical := canonicalFinish(finish)
	if canonical == "" || canonical == "nonfoil" {
		return id
	}
	return id + "_" + canonical
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
			// The date is not demanded: a gallery set the catalog has no
			// group for yet - Riot lists a set the morning before
			// TCGplayer opens it - has none to give, and refusing the
			// build over it would lose the nightly for every other set.
			if set.ID == "" || set.Name == "" {
				return 0, 0, 0, 0, fmt.Errorf("set %q (%s) missing identity", set.Name, set.ID)
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
	return 0, 0, 0, 0, errors.New("no card gallery blade in the output")
}

func countDatastore(data []byte) (baseline.Counts, error) {
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
	out := baseline.Counts{BySet: map[string]int{}}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	for _, blade := range doc.PageProps.Page.Blades {
		if blade.Type != "riftboundCardGallery" {
			continue
		}
		out.Cards = len(blade.Cards.Items)
		out.Sealed = len(blade.Sealed.Items)
		for _, card := range blade.Cards.Items {
			out.BySet[card.Set.Value.ID]++
		}
		return out, nil
	}
	return out, errors.New("no card gallery blade")
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 89 (required)")
	galleryPath := flag.String("gallery", "", "card-gallery payload file (default: fetch the live gallery)")
	against := flag.String("against", "", "baseline datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.01, "the share of its cards or sealed products a build may lose")
	baselineFit := flag.String("baseline-fit", "", "write this file when the build is fit to become the baseline the next build compares against")
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
	bothFinishes := catalogFinishes(&catalog)
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
	// Each level is checked on its own: a payload reshaped at the top -
	// the empty document a bad fetch can hand over - would otherwise stop
	// the build with a nil-interface panic rather than a sentence.
	pageProps, _ := doc["pageProps"].(map[string]any)
	page, _ := pageProps["page"].(map[string]any)
	blades, _ := page["blades"].([]any)
	if blades == nil {
		log.Fatalln("gallery payload: no pageProps.page.blades to read; the page's shape has changed")
	}
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
	// The gallery's two tables are the shape the whole build reads; a
	// payload without them is not one we can go on from, so say which is
	// missing rather than panicking several lines later.
	sets, ok := gallery["sets"].(map[string]any)
	if !ok {
		log.Fatalln("the card gallery blade carries no sets table")
	}
	cards, ok := gallery["cards"].(map[string]any)
	if !ok {
		log.Fatalln("the card gallery blade carries no cards table")
	}
	setItems, ok := sets["items"].([]any)
	if !ok {
		log.Fatalln("the gallery's sets table carries no items")
	}
	cardItems, ok := cards["items"].([]any)
	if !ok {
		log.Fatalln("the gallery's cards table carries no items")
	}

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
	var galleryUnnamed int
	for _, c := range cardItems {
		item, ok := c.(map[string]any)
		if !ok {
			galleryUnnamed++
			continue
		}
		set, _ := item["set"].(map[string]any)
		value, _ := set["value"].(map[string]any)
		setID, _ := value["id"].(string)
		code, _ := item["publicCode"].(string)
		if setID == "" || code == "" {
			galleryUnnamed++
			continue
		}
		if galleryByNumber[setID] == nil {
			galleryByNumber[setID] = map[string]map[string]any{}
		}
		galleryByNumber[setID][numberOf(code)] = item
	}
	if galleryUnnamed > 0 {
		log.Printf("gallery: %d printings name no set and number, and index nothing", galleryUnnamed)
	}

	// Process in a stable order so unchanged data produces byte-identical
	// output (consumers cache the file by etag).
	groups := catalog.Groups
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Abbreviation < groups[j].Abbreviation
	})

	// Printings the catalog names no finish for, whose gallery finishes
	// therefore stand. Zero today, and it should stay that way.
	var unpriced int
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
				// The qualifiers the catalog writes on the product, which
				// the gallery row has none of: Riot publishes a card under
				// its plain name, and what tells one printing of it from
				// another is written by whoever sells them. Adopted
				// printings have carried these all along; a printing the
				// gallery happened to publish was losing them for no reason
				// but the branch it arrived on - 91 overnumbered printings
				// and 102 alternate arts among them.
				if _, qualifiers := splitQualifiers(product.Name); len(qualifiers) > 0 {
					number := numberFor(product)
					named, _ := item["name"].(string)
					qualifiers = unnamedQualifiers(qualifiers, named)
					if promoTypes := promoTypesOf(qualifiers, number); len(promoTypes) > 0 {
						item["promoTypes"] = promoTypes
						item["variant"] = strings.Join(keptQualifiers(qualifiers, number), " ")
					}
				}
				// The catalog decides which finishes exist: a printing it
				// prices a sku for is one that exists, and the gallery says
				// nothing about finish at all. It can only fall back to the
				// gallery's own when the catalog names no finish for the
				// product, which means skus under a printing name this
				// build does not know - said out loud, because the gallery
				// answering for commerce is exactly what it cannot do.
				if f := finishes[product.ProductID]; len(f) > 0 {
					item["finishes"] = f
				} else {
					unpriced++
					log.Printf("%q (%d) has no finish in the catalog; the gallery's stand",
						product.Name, product.ProductID)
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
		var added int
		for _, product := range products {
			if !slices.Contains(tcgSingles, product.ProductType) {
				continue
			}
			number := numberFor(product)
			collector := 0
			// A number that is not a bare ordinal leaves collector at
			// zero.
			_, _ = fmt.Sscanf(strings.TrimLeft(product.Extended("Number"), "0"), "%d", &collector)

			// The parenthetical qualifiers become promo types, so sibling
			// promos share one clean name and are told apart by number or
			// by the storefront's own wording matching the types. A
			// qualifier the name already carries is the name, on this path
			// as on the stamped one.
			name, qualifiers := splitQualifiers(product.Name)
			qualifiers = unnamedQualifiers(qualifiers, name)
			kept := keptQualifiers(qualifiers, number)
			promoTypes := promoTypesOf(qualifiers, number)

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
					"url": emit.ImageURL(product.ImageURL),
				},
			}
			if len(promoTypes) > 0 {
				item["promoTypes"] = promoTypes
				item["variant"] = strings.Join(kept, " ")
			}
			cardItems = append(cardItems, item)
			added++
		}
		if added == 0 {
			continue
		}

		// No collectorNumberMax: a minted set's cards keep the numbers of
		// the sets they were reprinted from - PR-293a is Origins' 293 - and
		// the highest of those is another set's size, not this one's. The
		// gallery states a size for the sets it publishes and this build
		// invents none for the rest.
		set := map[string]any{
			"id":          group.Abbreviation,
			"name":        group.Name,
			"releaseDate": group.ReleaseDate(),
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

	if unpriced > 0 {
		log.Printf("finishes: %d printings the catalog names none for; the gallery's stand", unpriced)
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
					"url": emit.ImageURL(product.ImageURL),
				},
			})
		}
	}
	if len(sealedItems) > 0 {
		gallery["sealed"] = map[string]any{"items": sealedItems}
	}
	log.Printf("sealed: %d products", len(sealedItems))

	// The fields every other datastore here carries, added beside the
	// gallery's own rather than in place of them. This file is the upstream
	// payload with our data merged in, which is what lets the loader read it
	// unchanged - and it is also why a consumer holding it has to know a
	// second vocabulary for facts every other game states plainly: the set
	// is an object rather than a code, the picture is "cardImage", the
	// product id is bare where everyone else wraps it, and the labels have
	// no joined string beside them at all. Adding the common names costs
	// the loader nothing and spares every reader the special case.
	var stamped, variants, printings int
	for _, raw := range cardItems {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if set, ok := item["set"].(map[string]any); ok {
			if value, ok := set["value"].(map[string]any); ok {
				if id, ok := value["id"].(string); ok && id != "" {
					item["setCode"] = id
				}
			}
		}
		if code, ok := item["publicCode"].(string); ok {
			if number := numberWritten(code); number != "" {
				item["number"] = number
			}
		}
		if image, ok := item["cardImage"].(map[string]any); ok {
			if url, ok := image["url"].(string); ok && url != "" {
				item["image"] = url
			}
		}
		if id, ok := item["tcgplayerProductId"].(float64); ok && id != 0 {
			item["externalLinks"] = map[string]any{"tcgPlayerId": int(id)}
		} else if id, ok := item["tcgplayerProductId"].(int); ok && id != 0 {
			item["externalLinks"] = map[string]any{"tcgPlayerId": id}
		}
		if _, found := item["variant"]; found {
			variants++
		}
		// The uuid each finish prices, named here rather than left to the
		// loader to spell by joining a finish to the id. A uuid is what a
		// price is keyed on, and one spelled in the matcher moves whenever
		// the matcher changes how it spells a finish - silently, since a
		// uuid nobody stored resolves to nothing rather than erroring.
		// Named here, it moves only when this build says so.
		sold := stringsOf(item["finishes"])
		if len(sold) == 0 {
			// A printing the catalog sells nothing for names no finish,
			// and the loader reads it as sold in both. Saying so is what
			// keeps the uuids it reaches for from being invented - in the
			// catalog's own words, since it is the catalog that names its
			// printings.
			sold = bothFinishes
		}
		// One entry per printing, carrying the finish as TCGplayer prices
		// it - that being the name the datastore uses for it everywhere
		// else - beside the uuid it is quoted by. The uuid keeps the
		// matcher's spelling, which is the one already in circulation.
		//
		// It replaces the printingIds map and the finishes list both, which
		// were the same set of printings said twice: one naming them, the
		// other naming what each is called.
		sequence := make([]any, 0, len(sold))
		for _, finish := range sold {
			sequence = append(sequence, map[string]any{
				"finish": finish,
				"id":     printingUUID(fmt.Sprint(item["id"]), finish),
			})
		}
		item["printings"] = sequence
		delete(item, "finishes")
		printings += len(sequence)
		stamped++
	}
	log.Printf("common fields: %d cards given a setCode, number, image and product link; %d given a variant",
		stamped, variants)
	log.Printf("printings: %d named over %d cards, so the loader spells no uuid and reads no finish list beside them", printings, stamped)

	// The base run's size, under the name Pokemon and mtgjson give it. The
	// gallery calls it collectorNumberMax and Lorcana's upstream calls it
	// cardCounts.base; the fact is the same one.
	var sized int
	for _, raw := range setItems {
		set, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch maxNumber := set["collectorNumberMax"].(type) {
		case float64:
			if maxNumber > 0 {
				set["baseSetSize"] = int(maxNumber)
				sized++
			}
		case int:
			if maxNumber > 0 {
				set["baseSetSize"] = maxNumber
				sized++
			}
		}
	}
	log.Printf("sets: %d given a baseSetSize beside the gallery's collectorNumberMax", sized)
	var undated []string
	for _, entry := range setItems {
		set, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if date, _ := set["releaseDate"].(string); date == "" {
			undated = append(undated, fmt.Sprint(set["id"]))
		}
	}
	if len(undated) > 0 {
		sort.Strings(undated)
		log.Printf("sets: %d published by the gallery with no catalog group to date them yet: %s", len(undated), strings.Join(undated, " "))
	}

	sets["items"] = setItems
	cards["items"] = cardItems

	var buf bytes.Buffer
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	emit.PlainQuotes(doc)

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

	// Compare against the baseline, when the publish handed one over, and
	// say whether this build is fit to become the next one.
	if err := baseline.Guard(buf.Bytes(), countDatastore, baseline.Options{
		Against: *against, Tolerance: *againstTolerance, FitPath: *baselineFit, Unit: "printings",
	}); err != nil {
		log.Fatalln(err)
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
