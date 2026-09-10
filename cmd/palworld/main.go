// Command palworld builds the Palworld OFFICIAL CARD GAME datastore file,
// from the TCGplayer catalog dump for category 91.
//
// The datastore is the sum of both sources, the way every builder here is:
// every product the catalog types as a card, and every card palworldtcg.gg
// publishes from its mirror of Bushiroad's list. The catalog carries the
// identity for everything it sells - it is what the prices are keyed to -
// and the upstream half is what the game prints and TCGplayer does not
// list as a single.
//
// The two number the same card differently. TCGplayer writes "EBP01-001"
// and the upstream writes "BP01-001", and the E is not TCGplayer's
// invention: Bushiroad's English site serves this card as EBP01-001 and
// its Japanese site as BP01-001, so the prefixed form is the number
// printed on the card TCGplayer sells and the bare one is the Japanese
// printing's. The join reads the two alike by setting the prefix aside,
// and every number this datastore publishes wears it, minted rows
// included.
//
// One entry per product and sku printing. This game files each rarity of a
// card at a collector number of its own - the base card at "EBP01-001" and
// its parallel at "EBP01-001SSP" - so unlike Gundam or Yu-Gi-Oh no two
// products share a number, and the number carries the same distinction the
// rarity field records.
//
// The name parentheticals TCGplayer decorates products with are told apart
// per collector number, the way cmd/onepiece and cmd/yugioh do it: a
// parenthetical every product of a number carries is part of the card's
// name, one only some of them carry is the variant label the matcher
// narrows on. Because every number here holds exactly one product, that
// election has nothing to compare and the redundancy rule does the work
// instead: a qualifier that restates the product's own rarity or repeats
// what the collector number already spells - "(TSR)" beside
// "ETD01-001TSR" - is dropped as an echo of the field that carries it, and
// what remains is a genuine label, the event or the pal a promo names.
//
// Sets are the catalog groups, coded from their abbreviations. A group
// holding no product at all is skipped rather than carried: an empty set
// is dead weight in every consumer, and its code stays claimed so no
// existing set's code moves while it waits for its first product.
//
// Every product the catalog types as a card becomes an entry, and validate
// refuses a build that left one out: a shape nobody has seen yet stops the
// publish instead of vanishing from the datastore. Sealed products are
// everything filed outside the singles type, by exclusion, so a product
// type TCGplayer adds later lands on the sealed side where it is noticed.
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
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mtgban/datastore-gen/internal/baseline"
	"github.com/mtgban/datastore-gen/internal/emit"

	"github.com/mtgban/go-tcgplayer"
)

const (
	palworldCategory = 91

	// palworldCardsURL is palworldtcg.gg's public card API, which needs no
	// key and pages its answers.
	palworldCardsURL = "https://palworldtcg.gg/api/v1/cards?limit=100"
)

// palworldCard is the slice of an upstream card this build reads: what a
// card is and where it is filed, and nothing that would republish the
// upstream's own work.
type palworldCard struct {
	Number   string   `json:"card_number"`
	Name     string   `json:"name"`
	SetCode  string   `json:"set_code"`
	Rarity   string   `json:"rarity"`
	CardType string   `json:"card_type"`
	Color    []string `json:"color"`
}

// palworldPage is one page of that API: the cards, and where the next page
// is when there is one.
type palworldPage struct {
	Data []palworldCard `json:"data"`
	Meta struct {
		Next string `json:"next"`
	} `json:"meta"`
}

// upstreamRarity spells the upstream's rarity codes the way the catalog
// spells the same rarities, so a minted entry's rarity reads like every
// other entry's rather than in a second vocabulary. Each pairing was
// checked against the counts: the catalog files exactly 12 Double Rare to
// the upstream's 12 RR, 34 Common to its 34 C, and one Super Special Soul
// to its one SSS.
var upstreamRarity = map[string]string{
	"C":   "Common",
	"U":   "Uncommon",
	"R":   "Rare",
	"RR":  "Double Rare",
	"PR":  "Promo",
	"TD":  "Trial Deck",
	"SSS": "Super Special Soul",
}

// upstreamSet maps the set code the upstream writes onto the catalog
// group's abbreviation where the two differ.
var upstreamSet = map[string]string{
	"PROMO": "PR",
}

// upstreamSetCode is the set code of ours an upstream card's set answers
// to: the code the catalog abbreviates the same set with, through the alias
// table where the two differ.
func upstreamSetCode(u palworldCard) string {
	abbreviation := u.SetCode
	if aliased, found := upstreamSet[abbreviation]; found {
		abbreviation = aliased
	}
	return setCodeOf(abbreviation)
}

// englishNumber is the collector number as the English printing carries it.
// Bushiroad numbers the English cards with an E the Japanese ones do not
// have, the upstream publishes the Japanese form, and the catalog publishes
// the English one; the datastore follows the card TCGplayer sells.
func englishNumber(number string) string {
	if number == "" || strings.HasPrefix(number, "E") {
		return number
	}
	return "E" + number
}

// japaneseNumber is the same number with the prefix set aside, which is what
// the two sources can be read alike by.
func japaneseNumber(number string) string {
	return strings.TrimPrefix(number, "E")
}

// upstreamClient bounds a fetch from the community mirror: a hung upstream
// hangs the publish otherwise, since http.Get waits forever.
var upstreamClient = &http.Client{Timeout: 3 * time.Minute}

// upstreamGet fetches a URL as this build, named, so the mirror's logs can
// tell it from a browser.
func upstreamGet(location string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "datastore-gen/1.0 (+https://github.com/mtgban/datastore-gen)")
	return upstreamClient.Do(req)
}

// fetchCards reads the whole card list, following the API's paging. A local
// path is read instead when one is given, so a build can be pinned to a
// file; such a file may hold either one page's object or a bare array.
func fetchCards(location string) ([]palworldCard, error) {
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") {
		data, err := os.ReadFile(location)
		if err != nil {
			return nil, err
		}
		var page palworldPage
		if err := json.Unmarshal(data, &page); err == nil && len(page.Data) > 0 {
			return page.Data, nil
		}
		var bare []palworldCard
		if err := json.Unmarshal(data, &bare); err != nil {
			return nil, err
		}
		return bare, nil
	}
	var all []palworldCard
	// A page count nothing sane reaches, so a server answering with a
	// cycle of next links stops the build rather than running forever.
	for i := 0; location != "" && i < 200; i++ {
		resp, err := upstreamGet(location)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", location, resp.Status)
		}
		var page palworldPage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		location = page.Meta.Next
	}
	return all, nil
}

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; everything else is sealed by exclusion.
var tcgSingles = tcgplayer.SinglesProductTypes(palworldCategory)

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)

// soulCardName is what every Soul card is called, which is why the label on
// one has to be read rather than joined to it. Ten of them carry the name
// and nothing else, so a qualifier on one either says which Pal is drawn on
// it or how it was handed out, and only the second kind is a promotion.
const soulCardName = "Soul"

// palNames are the Pals this datastore carries, read off the head of every
// card name: "Chillet - Dragon Whisperer" is Chillet, and a Soul card
// labelled "(Chillet)" is the Soul with Chillet on it. Read off the data
// rather than named, because the game's own card list is the list of Pals
// and it grows with every set.
func palNames(singles []single) map[string]bool {
	out := map[string]bool{}
	for _, s := range singles {
		head, _, _ := strings.Cut(s.baseName, " - ")
		if head = strings.TrimSpace(head); head != "" {
			out[strings.ToLower(head)] = true
		}
	}
	return out
}

// promoTypesOf is the labels a printing carries, one at a time and
// lowercased the way every datastore here spells a promo type. The Pal a
// Soul card pictures is not one: nothing promoted a Soul for having Nox on
// it, so the Pal stays the variant it already is.
func promoTypesOf(name string, quals []string, pals map[string]bool) []string {
	out := make([]string, 0, len(quals))
	for _, qual := range quals {
		tag := strings.ToLower(strings.Join(strings.Fields(qual), " "))
		if tag == "" {
			continue
		}
		if name == soulCardName && pals[tag] {
			continue
		}
		// The token is the slug: a promo type is what a query carries and a
		// consumer keys on, and the words a reader is shown are the variant
		// beside it, which this build already writes. The Pal check above
		// reads the words, so the slugging happens after it.
		if slug := emit.PromoSlug(tag); slug != "" && !slices.Contains(out, slug) {
			out = append(out, slug)
		}
	}
	return out
}

// single is a card product with its name taken apart: the base name, the
// collector number, and the parentheticals the election below decides the
// meaning of.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []string
}

// decompose splits a product name into the base name and its
// parentheticals, dropping the collector number worn as decoration.
func decompose(p tcgplayer.Product, num string) single {
	name := p.Name
	if num != "" {
		name = strings.ReplaceAll(name, " - "+num, "")
	}

	var quals []string
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if q == "" || strings.EqualFold(q, num) {
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

var nonAlnumRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// initialsOf is the rarity read as the shorthand a product name writes it
// with: "Over Super Rare" is the "(OSR)" a name carries.
func initialsOf(s string) string {
	var b strings.Builder
	for _, word := range strings.Fields(s) {
		for _, r := range word {
			b.WriteRune(r)
			break
		}
	}
	return strings.ToUpper(b.String())
}

// redundant reports whether a qualifier says only what another field on the
// entry already says. Two fields say it: the rarity, which a name restates
// outright ("(C+)" on a C+ printing), spells with "Rare" elided, or
// shorthands to its initials; and the collector number, whose own suffix
// some of these games repeat in the name ("...-001TSR" beside "(TSR)").
// Either way the entry keeps the field and drops the echo, so a query for
// the card's name is not asked to carry the rarity too.
func redundant(qual, rarity, number string) bool {
	q := strings.ToUpper(nonAlnumRe.ReplaceAllString(qual, ""))
	if q == "" {
		return false
	}
	r := strings.ToUpper(nonAlnumRe.ReplaceAllString(rarity, ""))
	if q == r || q+"RARE" == r || q == initialsOf(rarity) {
		return true
	}
	n := strings.ToUpper(nonAlnumRe.ReplaceAllString(number, ""))
	return n != "" && q != n && strings.HasSuffix(n, q)
}

// provenanceWords name a product rather than a card: the pack, set, box or
// event a printing was handed out in.
var provenanceWords = map[string]bool{
	"pack": true, "packs": true, "set": true, "sets": true,
	"collection": true, "championship": true, "championships": true,
	"tournament": true, "regionals": true, "promotion": true,
	"prize": true, "campaign": true, "box": true, "expo": true,
}

// provenance reports whether a qualifier says where a printing came from
// rather than which card it is. Such a qualifier may never be elected into
// a name, however many printings of a number carry it.
//
// The election reads a qualifier every printing of a number carries as part
// of the card's name, and a promo whose only printing came out of one box
// would have the box elected into the card's name - a name no storefront
// writes and no search for the card finds. Every number in this game holds
// a single product, so that is the shape the election would meet here
// every time it fired. A word list rather than a table of spellings,
// because the spellings are open-ended - every season brings another
// promotional box - while the words they are built from are not. Whole
// words only: a card named "(Full Package)" is not a pack.
func provenance(qual string) bool {
	for _, word := range wordRe.FindAllString(strings.ToLower(qual), -1) {
		if provenanceWords[word] {
			return true
		}
	}
	return false
}

var wordRe = regexp.MustCompile(`[a-z0-9']+`)

// idStem spells a collector number for the inside of a uuid: every run of
// anything but a letter or a digit becomes one dash, because a slash is a
// path separator wherever a uuid is written down.
func idStem(number string) string {
	return strings.ToLower(strings.Trim(nonAlnumRe.ReplaceAllString(number, "-"), "-"))
}

func setCodeOf(abbreviation string) string {
	return strings.Trim(nonAlnumRe.ReplaceAllString(abbreviation, "-"), "-")
}

// isPromoGroup reports whether a catalog group hands its cards out rather
// than selling them in packs of its own. The group name is the only thing
// that says so in this category, the way it is the only thing in Yu-Gi-Oh's:
// the rarity names the treatment a card wears and never the promotion that
// handed it out, so a rarity test finds no wholly promotional group here at
// all.
//
// The matcher has been reading the set name for this because the datastore
// never said it - "strings.Contains(strings.ToLower(set.Name), "promo")" - which is the same fact
// asserted twice, in the place that cannot see the catalog.
func isPromoGroup(group tcgplayer.Group) bool {
	return strings.Contains(strings.ToLower(group.Name), "promo")
}

// setCodes assigns every group a unique, non-empty set code. Codes are
// claimed in group-id order, so the group that claimed one keeps it bare
// and only a later arrival is marked: a set code then depends on the groups
// that came before it and never on the ones that come after, and an
// existing set keeps its code the day TCGplayer files a new group under an
// abbreviation it already uses. A blank abbreviation gets a code minted
// from the group id. Every repair is logged, because none of it is the
// catalog's own identity.
func setCodes(groups []tcgplayer.Group) map[int]string {
	ordered := append([]tcgplayer.Group(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].GroupID < ordered[j].GroupID
	})

	codes := map[int]string{}
	used := map[string]bool{}
	var minted, suffixed int
	for _, group := range ordered {
		code := setCodeOf(group.Abbreviation)
		if code == "" {
			code = fmt.Sprintf("G%d", group.GroupID)
			minted++
			log.Printf("%s: no abbreviation, set code %s minted", group.Name, code)
		}
		if used[code] {
			code = fmt.Sprintf("%s-%d", code, group.GroupID)
			suffixed++
			log.Printf("%s: abbreviation %s already taken, set code %s minted",
				group.Name, group.Abbreviation, code)
		}
		if used[code] {
			log.Fatalf("set code %s still not unique; refusing to guess further", code)
		}
		used[code] = true
		codes[group.GroupID] = code
	}
	log.Printf("set codes: %d minted for blank abbreviations, %d deduplicated", minted, suffixed)
	return codes
}

// idBase is the id stem an entry carries before its finish suffix: the
// collector number and the product id, so two products sharing a number
// still mint different ids. A product the game gives no number is carried
// on its product id alone.
func idBase(number string, productID int) string {
	stem := idStem(number)
	if stem == "" {
		return fmt.Sprintf("%d", productID)
	}
	return fmt.Sprintf("%s_%d", stem, productID)
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 91 (required)")
	palworldCards := flag.String("palworld-cards", palworldCardsURL, "palworldtcg.gg cards API or a file holding its answer")
	against := flag.String("against", "", "baseline datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.01, "the share of its cards or sealed products a build may lose")
	baselineFit := flag.String("baseline-fit", "", "write this file when the build is fit to become the baseline the next build compares against")
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
	if catalog.Category.CategoryID != palworldCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, palworldCategory)
	}
	log.Printf("catalog: %d groups, %d products", len(catalog.Groups), len(catalog.Products))

	groupByID := map[int]tcgplayer.Group{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	codes := setCodes(catalog.Groups)
	printings := catalog.PrintingNames()
	displayOrder := printingDisplayOrder(&catalog)

	// Split the products: every single becomes printings, the non-single
	// types become sealed.
	var singles []single
	var sealedProducts []tcgplayer.Product
	var unnumbered int
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			// Every card product the catalog carries prices at least one
			// sku, and a product with none has no printing to file an
			// entry under: stop rather than drop it.
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
	if len(singles) == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}
	log.Printf("singles: %d kept (%d without a collector number), %d sealed",
		len(singles), unnumbered, len(sealedProducts))

	// Drop the qualifiers that only echo a field the entry already
	// carries, before the election reads them: a rarity shorthand is never
	// part of a card's name however many printings of the number wear it.
	var echoes int
	for i := range singles {
		s := &singles[i]
		rarity := s.product.Extended("Rarity")
		var kept []string
		for _, q := range s.quals {
			if redundant(q, rarity, s.number) {
				echoes++
				continue
			}
			kept = append(kept, q)
		}
		s.quals = kept
	}
	log.Printf("qualifiers: %d dropped as an echo of the rarity or the collector number", echoes)

	// Per collector number within its group: a qualifier every product of
	// the number carries is part of the name, not a variant. A number with
	// a single product cannot make that call alone, so the name parts
	// learned from the multi-product numbers decide for it - the same form
	// or epithet decorates the number's every printing.
	byNumber := map[string][]*single{}
	for i := range singles {
		if singles[i].number == "" {
			continue
		}
		key := fmt.Sprintf("%d|%s", singles[i].product.GroupID, singles[i].number)
		byNumber[key] = append(byNumber[key], &singles[i])
	}
	nameParens := map[string]bool{}
	for _, bucket := range byNumber {
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
			if n == len(bucket) && !provenance(q) {
				nameParens[q] = true
			}
		}
	}
	var elected int
	for i := range singles {
		s := &singles[i]
		var name, variant []string
		name = append(name, s.baseName)
		for _, q := range s.quals {
			if nameParens[q] {
				name = append(name, "("+q+")")
				elected++
			} else {
				variant = append(variant, q)
			}
		}
		s.baseName = strings.Join(name, " ")
		s.quals = variant
	}
	log.Printf("qualifiers: %d elected into a name, %d distinct spellings elected",
		elected, len(nameParens))

	// Emit. Sets are the catalog groups that hold anything; a group with
	// no product is a husk TCGplayer keeps around for a set it has not
	// stocked yet, and a set nothing references is dead weight in every
	// consumer. Its code stays claimed above, so no existing set's code
	// moves while it is empty.
	productsIn := map[int]int{}
	for _, product := range catalog.Products {
		productsIn[product.GroupID]++
	}
	sets := map[string]any{}
	var populated, promoSets, skippedEmpty int
	for _, group := range catalog.Groups {
		if productsIn[group.GroupID] == 0 {
			skippedEmpty++
			continue
		}
		populated++
		set := map[string]any{
			"name":        group.Name,
			"releaseDate": group.ReleaseDate(),
		}
		// The type is what tells the matcher a printing is promotional, so
		// only the wholly promotional groups carry it.
		if isPromoGroup(group) {
			set["type"] = "promo"
			promoSets++
		}
		sets[codes[group.GroupID]] = set
	}
	log.Printf("promotional sets: %d of %d", promoSets, len(sets))
	if skippedEmpty > 0 {
		log.Printf("sets: %d empty groups hold no product and are skipped", skippedEmpty)
	}
	// The recount: one set per group that holds anything. A code claimed
	// twice would fold two groups onto one entry, and validate cannot see
	// it - the code still resolves for every card naming it, it just names
	// the wrong set.
	if len(sets) != populated {
		log.Fatalf("emitted %d sets for %d populated catalog groups; refusing to publish",
			len(sets), populated)
	}

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

	pals := palNames(singles)

	upstream, err := fetchCards(*palworldCards)
	if err != nil {
		log.Fatalln("palworldtcg:", err)
	}
	// Stable order, so unchanged data keeps producing byte-identical output.
	sort.Slice(upstream, func(i, j int) bool {
		return upstream[i].Number < upstream[j].Number
	})

	// A product the catalog files with no collector number is a card the
	// game does number, and the list upstream publishes says which: the
	// one Super Special Soul of the booster set is SOUL-002, and the
	// catalog's "Soul (SSS)" of that set at that rarity is it. Left
	// unnumbered, the product cannot cover its own number, and the mint
	// below adds the card a second time beside it, unpriced, with a
	// finish nothing sells it in. Only where upstream names exactly one
	// card of that set, name and rarity: two would be a guess.
	var numbered int
	for i := range singles {
		s := &singles[i]
		if s.number != "" {
			continue
		}
		code := codes[s.product.GroupID]
		rarity := s.product.Extended("Rarity")
		var matches []palworldCard
		for _, u := range upstream {
			if upstreamSetCode(u) != code || !strings.EqualFold(u.Name, s.baseName) || upstreamRarity[u.Rarity] != rarity {
				continue
			}
			matches = append(matches, u)
		}
		if len(matches) != 1 {
			log.Printf("number: %q (%d) carries no collector number and upstream names %d cards of that name and rarity in %s; kept unnumbered",
				s.product.Name, s.product.ProductID, len(matches), code)
			continue
		}
		s.number = englishNumber(matches[0].Number)
		numbered++
		log.Printf("number: %q (%d) carries no collector number; upstream files the %s %q of %s at %s",
			s.product.Name, s.product.ProductID, rarity, s.baseName, code, s.number)
	}
	if numbered > 0 {
		log.Printf("number: %d unnumbered products took their number from upstream", numbered)
	}

	var cards []any
	for _, s := range singles {
		productID := s.product.ProductID
		for _, finish := range emit.OrderedFinishes(printings[productID], displayOrder) {
			entry := map[string]any{
				"id":      idBase(s.number, productID) + emit.FinishSuffix(finish),
				"name":    s.baseName,
				"setCode": codes[s.product.GroupID],
				"rarity":  s.product.Extended("Rarity"),
				"finish":  finish,
				"image":   emit.ImageURL(s.product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if s.number != "" {
				entry["number"] = s.number
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
				if tags := promoTypesOf(s.baseName, s.quals, pals); len(tags) > 0 {
					entry["promoTypes"] = tags
				}
			}
			if t := s.product.Extended("CardType"); t != "" {
				entry["type"] = t
			}
			if c := s.product.Extended("Color"); c != "" {
				entry["color"] = c
			}
			cards = append(cards, entry)
		}
	}

	// The other half of the datastore: the cards the game prints that
	// TCGplayer sells no single of. A minted entry names no product
	// because there is none - nothing prices it - and it is carried so a
	// listing of one resolves rather than falling through to whatever
	// shares its number. Its id holds no product id at all, which is what
	// keeps the two namespaces apart: a catalog id always carries
	// "_<product id>" before its finish suffix and a minted one never can.
	// The catalog files each rarity at a number of its own and the
	// upstream folds them into the base card, so a base number the catalog
	// sells covers every printing upstream knows about.
	carriedNumbers := map[string]bool{}
	for _, s := range singles {
		if s.number != "" {
			carriedNumbers[japaneseNumber(s.number)] = true
		}
	}
	var minted, unplaced, unrated int
	mintedIDs := map[string]bool{}
	for _, u := range upstream {
		if u.Number == "" || carriedNumbers[japaneseNumber(u.Number)] {
			continue
		}
		code := upstreamSetCode(u)
		if _, known := sets[code]; !known {
			// A card whose set this datastore does not carry has nowhere
			// to be filed, and a set invented for it would be a set no
			// product references. Logged rather than dropped silently.
			unplaced++
			log.Printf("palworldtcg: %s (%s) names set %q, which holds no product here; not minted",
				u.Number, u.Name, u.SetCode)
			continue
		}
		number := englishNumber(u.Number)
		id := idStem(number)
		if id == "" || mintedIDs[id] {
			unplaced++
			log.Printf("palworldtcg: %s (%s) mints no usable id; not minted", u.Number, u.Name)
			continue
		}
		mintedIDs[id] = true
		rarity := u.Rarity
		if spelled, known := upstreamRarity[rarity]; known {
			rarity = spelled
		} else if rarity != "" {
			unrated++
		}
		entry := map[string]any{
			"id":      id,
			"name":    u.Name,
			"number":  number,
			"setCode": code,
			"rarity":  rarity,
			"finish":  emit.PlainPrinting(&catalog),
		}
		if u.CardType != "" {
			entry["type"] = u.CardType
		}
		if len(u.Color) > 0 {
			entry["color"] = strings.Join(u.Color, " ")
		}
		cards = append(cards, entry)
		minted++
	}
	// The direction nothing else counts: an upstream card this datastore
	// does not hold would be invisible, since the coverage invariant only
	// looks at the catalog side.
	log.Printf("palworldtcg: %d cards upstream, %d minted for printings TCGplayer sells no single of (%d unplaced, %d carrying a rarity code this build does not spell)",
		len(upstream), minted, unplaced, unrated)

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []any
	for _, product := range sealedProducts {
		group := groupByID[product.GroupID]
		sealed = append(sealed, map[string]any{
			"id":          fmt.Sprintf("%s-%d", strings.ToLower(codes[group.GroupID]), product.ProductID),
			"name":        product.Name,
			"setCode":     codes[group.GroupID],
			"releaseDate": group.ReleaseDate(),
			"image":       emit.ImageURL(product.ImageURL),
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
		"game":   "palworld",
		"sets":   sets,
		"cards":  cards,
		"sealed": sealed,
	}
	var buf bytes.Buffer
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	emit.PlainQuotes(doc)

	if err := json.NewEncoder(&buf).Encode(doc); err != nil {
		log.Fatalln(err)
	}

	// Re-read the encoded output and verify it structurally before
	// publishing anything: a format drift or a truncated dump must fail
	// here, not in every consumer.
	counted, err := validate(buf.Bytes(), catalogFinishes)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d sealed", counted.sets, counted.cards, counted.sealed)
	if counted.cards != len(cards) || counted.sealed != len(sealed) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			len(cards), len(sealed), counted.cards, counted.sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a single, so it is exhaustive by
	// construction and cannot lose a product to a rule that did not know
	// what to do with it - the card side's whole failure mode. What it can
	// lose a product to is an edit: one `continue` on the sealed path and
	// the products would leave the datastore with nothing to say so.
	wantSealed := len(catalog.Products) - len(singles)
	if counted.sealed != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			counted.sealed, wantSealed)
	}

	// Compare against the baseline, when the publish handed one over, and
	// say whether this build is fit to become the next one.
	if err := baseline.Guard(buf.Bytes(), baseline.Count, baseline.Options{
		Against: *against, Tolerance: *againstTolerance, FitPath: *baselineFit,
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
		if _, found := got[productID]; !found {
			missing = append(missing, productID)
		}
	}
	for productID := range got {
		if _, found := want[productID]; !found {
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

// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// idShape is what a uuid has to look like wherever one is written down: a
// slash is a path separator and a space ends a word, and a uuid travels
// through urls, filenames and query strings alike.
var idShape = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// validate decodes an encoded datastore and checks its shape: every card
// and sealed product carrying its identity, every id unique within its
// namespace, no two entries wearing the same identity, every referenced set
// existing, every finish one of the printing names, and every product's
// entries covering exactly the sku printings the catalog lists for it.
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
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"cards"`
		Sealed []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"sealed"`
	}
	var out counts
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}

	if doc.Game != "palworld" {
		return out, fmt.Errorf("game is %q, not palworld", doc.Game)
	}
	for code, set := range doc.Sets {
		if set.Name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cardIDs := map[string]bool{}
	// A query resolves a card by its name, number, set, rarity and variant
	// label, never by the id, so two products wearing all of them alike are
	// one card to every consumer and would alias each other's prices.
	// Rarity is in the key even though this game files each rarity at a
	// collector number of its own, so the number already separates them:
	// the day a product is filed at a number it shares, the rarity is what
	// keeps the pair from reading as one card. The key holds the product id rather than
	// a flag so a product's own Normal and Foil entries pass while two
	// different products never do.
	identities := map[string]string{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		// The number is not required: the game hands out cards it gives no
		// collector number, and those are carried on the id their product
		// alone mints.
		if card.ID == "" || card.Name == "" || card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q",
				card.Name, card.ID, card.Number)
		}
		if card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) carries no finish at all", card.Name, card.ID)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		identity := strings.Join([]string{
			card.Name, card.Number, card.SetCode, card.Rarity, card.Variant}, "|")
		// A minted printing sells as no product, so it stands for itself
		// under its own uuid; keying those on the absent product id would
		// make every one of them the same card and wave through exactly
		// the collision this catches.
		bearer := fmt.Sprintf("product %d", card.ExternalLinks.TcgPlayerID)
		if card.ExternalLinks.TcgPlayerID == 0 {
			bearer = "card " + card.ID
		}
		if other, seen := identities[identity]; seen && other != bearer {
			return out, fmt.Errorf("%s and %s wear one identity: %s", other, bearer, identity)
		}
		identities[identity] = bearer
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		// Only products are counted against the catalog's skus: a minted
		// printing answers to no product and would otherwise pile its
		// finish under product 0, which coverage would then have to
		// explain.
		if productID := card.ExternalLinks.TcgPlayerID; productID != 0 {
			if sliceContains(gotFinishes[productID], card.Finish) {
				return out, fmt.Errorf("product %d carries finish %q twice", productID, card.Finish)
			}
			gotFinishes[productID] = append(gotFinishes[productID], card.Finish)
		}
	}
	if err := coverage(gotFinishes, wantFinishes); err != nil {
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
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerID == 0 {
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

// printingDisplayOrder is where each of a category's printings sits in the
// order TCGplayer displays them.
func printingDisplayOrder(c *tcgplayer.CatalogDump) map[string]int {
	rank := map[string]int{}
	for _, p := range c.Printings {
		rank[p.Name] = p.DisplayOrder
	}
	return rank
}
