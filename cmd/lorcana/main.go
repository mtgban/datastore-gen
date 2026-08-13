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
// D100) are not appended as new cards: LorcanaJSON already carries them,
// filed under the set they belong to, and the id fill above maps the
// catalog's promo products onto those printings by name and number. Minting
// our own entries would create an id space that collides with LorcanaJSON's
// integer ids the day upstream publishes the real card. The audit below
// counts the singles that match nothing, so a gap upstream never covers
// would be noticed rather than assumed away.
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
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	// lorcanaCategory is Lorcana's TCGplayer category, the one the catalog
	// dump is expected to carry.
	lorcanaCategory = 71

	// tcgSingles is the product type single cards are filed under.
	// Everything else the catalog carries is a sealed product: the
	// comparison is against the singles type rather than a list of sealed
	// ones, so a type TCGplayer adds later lands on the sealed side where
	// it is noticed instead of silently passing as a single.
	tcgSingles = "Cards"
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
	// Skus enumerate every printing/condition/language a product is sold in.
	// Only the printing matters here, and it is the whole reason the catalog
	// dump is read instead of a price feed: a printing exists whether or not
	// anyone happens to be selling it today.
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

// releaseDate reduces a group's publishedOn timestamp to the bare day
// LorcanaJSON dates carry ("2023-08-18T00:00:00" -> "2023-08-18").
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

// printingNames maps each product to the sorted printing names it is sold
// under. TCGplayer's category 71 has exactly three — Normal, Holofoil and
// Cold Foil — and a printing it does not list for a product is one that
// does not exist.
func (c *tcgCatalog) printingNames() map[int][]string {
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

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 3000, "refuse to emit a datastore with fewer cards")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 71 (required)")
	source := flag.String("lorcana", "", "LorcanaJSON allCards file, path or URL (required)")
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
	var catalog tcgCatalog
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != lorcanaCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, lorcanaCategory)
	}
	var singles int
	productByID := map[int]tcgProduct{}
	for _, product := range catalog.Products {
		productByID[product.ProductID] = product
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
	printings := catalog.printingNames()
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
		key := normalizeName(product.Name) + "|" + number(product.extended("Number"))
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
	unclaimed := map[string][]tcgProduct{}
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles || claimed[product.ProductID] {
			continue
		}
		num := product.extended("Number")
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

	// Audit what remains unmatched: a single the catalog carries that no
	// card claimed or matched. Today these are the presale printings
	// upstream has not published yet and the split listings of cards that
	// already carry an id; a growing count means upstream stopped covering
	// something and the no-minting decision above needs revisiting.
	remaining := map[string]int{}
	groupByID := map[int]tcgGroup{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	for _, products := range unclaimed {
		for _, product := range products {
			if matched[product.ProductID] {
				continue
			}
			remaining[groupByID[product.GroupID].Abbreviation]++
		}
	}
	if len(remaining) > 0 {
		log.Printf("unmatched singles by group: %v", remaining)
	}

	// Sealed products: everything the catalog files outside the singles
	// type, from every group, in a top-level array a stock LorcanaJSON
	// reader ignores. Groups LorcanaJSON has no set for (the promotional
	// ones) get a set entry minted so every sealed product's set exists.
	groups := append([]tcgGroup(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Abbreviation < groups[j].Abbreviation
	})
	productsByGroup := map[int][]tcgProduct{}
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
			if product.ProductType == tcgSingles {
				continue
			}
			sealedItems = append(sealedItems, map[string]any{
				"id":          fmt.Sprintf("%s-%d", strings.ToLower(group.Abbreviation), product.ProductID),
				"name":        product.Name,
				"setCode":     group.Abbreviation,
				"releaseDate": group.releaseDate(),
				"image":       imageURL(product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": product.ProductID,
				},
			})
			count++
		}
		if count == 0 {
			continue
		}
		if _, found := sets[group.Abbreviation]; !found {
			sets[group.Abbreviation] = map[string]any{
				"name":        group.Name,
				"releaseDate": group.releaseDate(),
				"type":        "promo",
			}
			log.Printf("%s (%s): set minted for %d sealed products", group.Name, group.Abbreviation, count)
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
	counted, err := validate(buf.Bytes())
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d tcgplayer ids, %d sealed",
		counted.sets, counted.cards, counted.identified, counted.sealed)
	if counted.cards != len(cards) || counted.sealed != len(sealedItems) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			len(cards), len(sealedItems), counted.cards, counted.sealed)
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
	sets, cards, sealed, identified int
}

// validate decodes an encoded datastore and checks its shape: sets and
// cards present, every card and sealed product carrying its identity,
// every id unique within its namespace, every sealed set existing.
func validate(data []byte) (counts, error) {
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
