// Command gundam builds the Gundam Card Game datastore file, from the
// TCGplayer catalog dump for category 86.
//
// The datastore is the sum of both sources, the way every builder here is:
// every product the catalog types as a card, and every card yzRobo's
// gcg-api publishes, which mirrors Bandai's own card list weekly. The
// catalog carries the identity for everything it sells - it is what the
// prices are keyed to - and the upstream half is what the game prints and
// TCGplayer does not list as a single: the EX Base, EX Resource and
// Resource cards handed out with decks and boxes. Those are minted, naming
// no product because none exists.
//
// No upstream image is stored and no rules text: the catalog's own images
// are what the datastore carries, and gcg-api publishes under no clear
// licence, so what is taken from it is the fact that a card exists and the
// identity it exists under.
//
// One entry per product and sku printing. Rarity is the variant axis - the
// same collector number appears under several rarities as separate
// products, "Common" beside "C+" beside "C++" - so the rarity field tells
// those printings apart and the id carries the product it came from.
//
// The name parentheticals TCGplayer decorates products with are told apart
// per collector number, the way cmd/onepiece and cmd/yugioh do it: a
// parenthetical every product of a number carries is part of the card's
// name - the mobile suit's form, "(MA Mode)", "(Destroy Mode)", which is
// what the card is called - and one only some of them carry is the variant
// label the matcher narrows on. A qualifier that merely restates the
// product's own rarity ("(C+)" on a C+ printing) or repeats what the
// collector number already spells is dropped as redundant with the field
// that carries it.
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

	"github.com/mtgban/go-tcgplayer"
)

const (
	gundamCategory = 86

	// gcgCardsURL is yzRobo/gcg-api's weekly card dump, the closest thing
	// this game has to an official list in machine-readable form.
	gcgCardsURL = "https://raw.githubusercontent.com/yzRobo/gcg-api/main/data/cards.json"
)

// gcgCard is the slice of a gcg-api card this build reads: what a card is
// and where it is filed, and nothing that would republish the upstream's
// own work.
type gcgCard struct {
	Number   string `json:"card_number"`
	Name     string `json:"name"`
	SetCode  string `json:"set_code"`
	Rarity   string `json:"rarity"`
	CardType string `json:"card_type"`
	Color    string `json:"color"`
}

// fetch reads a local path or an http location, so a build can be pinned to
// a file and the default can be the live URL.
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
		return nil, fmt.Errorf("%s: %s", location, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; everything else is sealed by exclusion.
var tcgSingles = tcgplayer.SinglesProductTypes(gundamCategory)

// finishSuffix maps each sku printing name to the suffix its entry's id
// carries. Any other printing name is a hard failure, because a suffix
// invented on the fly would not be a stable identity.
var finishSuffix = map[string]string{
	"Normal":   "",
	"Holofoil": "_holo",
}

// finishOrder fixes the order a product's entries are emitted in.
var finishOrder = []string{
	"Normal",
	"Holofoil",
}

// upstreamRarity spells gcg-api's one and two letter rarity codes the way
// the catalog spells the same rarities, so a minted entry's rarity field
// reads like every other entry's rather than in a second vocabulary. A code
// this does not know is carried through as it stands and logged, because a
// rarity guessed at would be worse than one spelled oddly.
var upstreamRarity = map[string]string{
	"C":  "Common",
	"U":  "Uncommon",
	"R":  "Rare",
	"LR": "Legend Rare",
	"P":  "Promo",
}

// imageURL asks the catalog's CDN for the larger rendition. The dump
// carries the thumbnail size; the same URL one size up is the card.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)

// spelledNumberRe is a collector number of this game's shape written into a
// product name: "EX Resource (EXR-003)", "Resource (RP-046)".
var spelledNumberRe = regexp.MustCompile(`\(([A-Z]{1,5}-\d{1,4})\)`)

// numberFor is the collector number a product carries. The catalog files it
// in a field and, for the resource and token cards, spells it into the
// product name as well; where the two disagree the name is taken, because
// the name is what somebody reading the card wrote down and the field is
// what somebody typing a row filled in.
//
// It disagrees twice in this catalog against 247 agreements, and both
// disagreements are a field that repeats the row above it: "EX Resource
// (EXR-003)" is filed under EXR-002, which two other products already
// cover, and "Resource (RP-046)" under RP-045, in a run whose names read
// 045, 046, 047, 048, 049. The upstream corroborates the first outright -
// it puts EXR-002 in set ST09 and EXR-003 in ST10, and this product is in
// ST10. Left alone, each mis-numbered product prices a card it is not and
// the card it really is has no price at all: the number it belongs to gets
// minted from the upstream instead, sold by nobody.
func numberFor(p tcgplayer.Product) string {
	number := p.Extended("Number")
	if strings.EqualFold(number, "N/A") {
		number = ""
	}
	spelled := spelledNumberRe.FindStringSubmatch(p.Name)
	if spelled != nil && number != "" && spelled[1] != number {
		log.Printf("number: %q (%d) is filed under %s and named %s; taking the name",
			p.Name, p.ProductID, number, spelled[1])
		return spelled[1]
	}
	return number
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
// of the card's name, which is right for the mobile suit's form and its
// faction - "(MA Mode)", "(Sleeves)" - and wrong for a promo whose only
// printings came out of one box: "Resource (RP-045) (EVX07 Resource Set)"
// is the same Resource card the set sells, handed out in a resource set,
// and a name carrying that is a name no storefront writes and no search
// for the card finds. A word list rather than a table of spellings,
// because the spellings are open-ended - every season brings another judge
// pack - while the words they are built from are not. Whole words only: a
// card named "(Full Package)" is not a pack.
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

// datastoreCounts is what a datastore holds: the two totals, and the card
// count per set. It is read off an encoded datastore - this build's own, or
// the one it is about to replace - so both sides are counted the same way
// by the same code.
type datastoreCounts struct {
	cards, sealed int
	bySet         map[string]int
}

func countDatastore(data []byte) (datastoreCounts, error) {
	var doc struct {
		Cards []struct {
			SetCode string `json:"setCode"`
		} `json:"cards"`
		Sealed []struct{} `json:"sealed"`
	}
	out := datastoreCounts{bySet: map[string]int{}}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	out.cards = len(doc.Cards)
	out.sealed = len(doc.Sealed)
	for _, card := range doc.Cards {
		out.bySet[card.SetCode]++
	}
	return out, nil
}

// regression compares this build against the datastore it is about to
// replace and refuses to publish one that lost a meaningful share of it.
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
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 86 (required)")
	gcgCards := flag.String("gcg-cards", gcgCardsURL, "gcg-api cards file, path or URL")
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
	if catalog.Category.CategoryID != gundamCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, gundamCategory)
	}
	log.Printf("catalog: %d groups, %d products", len(catalog.Groups), len(catalog.Products))

	groupByID := map[int]tcgplayer.Group{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	codes := setCodes(catalog.Groups)
	printings := catalog.PrintingNames()

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
		num := numberFor(product)
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
	var populated, skippedEmpty int
	for _, group := range catalog.Groups {
		if productsIn[group.GroupID] == 0 {
			skippedEmpty++
			continue
		}
		populated++
		sets[codes[group.GroupID]] = map[string]any{
			"name":        group.Name,
			"releaseDate": group.ReleaseDate(),
		}
	}
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

	var cards []any
	for _, s := range singles {
		productID := s.product.ProductID
		for _, finish := range finishOrder {
			if !sliceContains(printings[productID], finish) {
				continue
			}
			entry := map[string]any{
				"id":      idBase(s.number, productID) + finishSuffix[finish],
				"name":    s.baseName,
				"setCode": codes[s.product.GroupID],
				"rarity":  s.product.Extended("Rarity"),
				"finish":  finish,
				"image":   imageURL(s.product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if s.number != "" {
				entry["number"] = s.number
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
			}
			if t := s.product.Extended("CardType"); t != "" {
				entry["type"] = t
			}
			if c := s.product.Extended("Color"); c != "" {
				entry["color"] = c
			}
			cards = append(cards, entry)
		}
		// A printing name the suffix table does not know would otherwise
		// leave the product with fewer entries than it has skus, which
		// validate reports as a coverage failure without saying why.
		for _, name := range printings[productID] {
			if _, known := finishSuffix[name]; !known {
				log.Fatalf("unknown sku printing %q on %q (%d); the id suffix for it has to be decided, not invented",
					name, s.product.Name, productID)
			}
		}
	}

	// The other half of the datastore: the cards the game prints that
	// TCGplayer sells no single of. A minted entry names no product
	// because there is none - nothing prices it - and it is carried so a
	// listing of one resolves rather than falling through to whatever
	// shares its number. Its id holds no product id at all, which is what
	// keeps the two namespaces apart: a catalog id always carries
	// "_<product id>" before its finish suffix and a minted one never can.
	upstreamData, err := fetch(*gcgCards)
	if err != nil {
		log.Fatalln("gcg-api:", err)
	}
	var upstream []gcgCard
	if err := json.Unmarshal(upstreamData, &upstream); err != nil {
		log.Fatalln("gcg-api:", err)
	}
	carriedNumbers := map[string]bool{}
	for _, s := range singles {
		if s.number != "" {
			carriedNumbers[s.number] = true
		}
	}
	// Stable order, so unchanged data keeps producing byte-identical output.
	sort.Slice(upstream, func(i, j int) bool {
		return upstream[i].Number < upstream[j].Number
	})
	var minted, unplaced, unrated int
	mintedIDs := map[string]bool{}
	for _, u := range upstream {
		if u.Number == "" || carriedNumbers[u.Number] {
			continue
		}
		code := setCodeOf(u.SetCode)
		if _, known := sets[code]; !known {
			// A card whose set this datastore does not carry has nowhere
			// to be filed, and a set invented for it would be a set no
			// product references. Logged rather than dropped silently.
			unplaced++
			log.Printf("gcg-api: %s (%s) names set %q, which holds no product here; not minted",
				u.Number, u.Name, u.SetCode)
			continue
		}
		id := idStem(u.Number)
		if id == "" || mintedIDs[id] {
			unplaced++
			log.Printf("gcg-api: %s (%s) mints no usable id; not minted", u.Number, u.Name)
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
			"number":  u.Number,
			"setCode": code,
			"rarity":  rarity,
			"finish":  "Normal",
		}
		if u.CardType != "" {
			entry["type"] = u.CardType
		}
		if u.Color != "" {
			entry["color"] = u.Color
		}
		cards = append(cards, entry)
		minted++
	}
	// The direction nothing else counts: an upstream card this datastore
	// does not hold would be invisible, since the coverage invariant only
	// looks at the catalog side.
	log.Printf("gcg-api: %d cards upstream, %d minted for printings TCGplayer sells no single of (%d unplaced, %d carrying a rarity code this build does not spell)",
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
			"image":       imageURL(product.ImageURL),
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
		"game":   "gundam",
		"sets":   sets,
		"cards":  cards,
		"sealed": sealed,
	}
	var buf bytes.Buffer
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	plainQuotes(doc)

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
	fit := true
	if *against != "" || *baselineFit != "" {
		current, err := countDatastore(buf.Bytes())
		if err != nil {
			log.Fatalln("against:", err)
		}
		if *against != "" {
			previousData, err := os.ReadFile(*against)
			if err != nil {
				log.Fatalln("against:", err)
			}
			previous, err := countDatastore(previousData)
			if err != nil {
				log.Fatalln("against:", err)
			}
			log.Printf("against %s: %d cards (was %d), %d sealed (was %d), %d sets (was %d)",
				*against, current.cards, previous.cards, current.sealed, previous.sealed,
				len(current.bySet), len(previous.bySet))
			if err := regression(previous, current, *againstTolerance); err != nil {
				log.Fatalln("against: refusing to publish:", err)
			}
			// The baseline only ever moves forward. A build smaller than
			// it - legitimately, within the tolerance - must not become
			// the thing the next build is measured against, or a run of
			// tolerated drops ratchets it down one step at a time and the
			// whole loss is never large enough for any single run to see.
			fit = current.cards >= previous.cards && current.sealed >= previous.sealed
		}
		if *baselineFit != "" {
			if !fit {
				log.Printf("baseline: unchanged, this build holds less than it does")
			} else {
				note := fmt.Sprintf("cards=%d sealed=%d\n", current.cards, current.sealed)
				if err := os.WriteFile(*baselineFit, []byte(note), 0o644); err != nil {
					log.Fatalln("baseline:", err)
				}
				log.Printf("baseline: this build becomes the one the next is measured against")
			}
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

	if doc.Game != "gundam" {
		return out, fmt.Errorf("game is %q, not gundam", doc.Game)
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
	// Rarity is in the key because it is this game's variant axis: the same
	// number is sold as "Common" and again as "C+", one card twice, and the
	// rarity is the only field that tells the two apart once the name's
	// echo of it has been dropped. The key holds the product id rather than
	// a flag so a product's own Normal and Holofoil entries pass while two
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
		if _, known := finishSuffix[card.Finish]; !known {
			return out, fmt.Errorf("card %q (%s) carries unknown finish %q", card.Name, card.ID, card.Finish)
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
		bearer := fmt.Sprintf("product %d", card.ExternalLinks.TcgPlayerId)
		if card.ExternalLinks.TcgPlayerId == 0 {
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
		if productID := card.ExternalLinks.TcgPlayerId; productID != 0 {
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
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerId == 0 {
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

// typographic is the quotes a catalog spells with and no consumer queries
// with.
var typographic = strings.NewReplacer(
	"‘", "'", "’", "'", "“", `"`, "”", `"`)

// plainQuotes rewrites those quotes wherever the document carries them. The
// catalogs are not consistent about it, and a query carries one spelling,
// so the card filed under the other cannot be found.
//
// The whole document is walked rather than the fields known to carry them,
// because the field that starts carrying them tomorrow would otherwise be
// missed, and it runs before the output is encoded so the check that
// re-reads it sees exactly what will be published.
func plainQuotes(v any) any {
	switch t := v.(type) {
	case string:
		return typographic.Replace(t)
	case map[string]any:
		for k, e := range t {
			t[k] = plainQuotes(e)
		}
	case []any:
		for i, e := range t {
			t[i] = plainQuotes(e)
		}
	}
	return v
}
