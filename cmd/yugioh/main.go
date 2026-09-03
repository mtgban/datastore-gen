// Command yugioh builds the Yu-Gi-Oh datastore file consumed by
// go-mtgban's mtgmatcher/yugioh loader, from the TCGplayer catalog dump
// for category 2, with set release dates enriched from YGOPRODeck's
// cardsets listing.
//
// Identity is the catalog's, one entry per product and sku printing.
// Rarity is the variant axis — the same collector number appears under
// several rarities as separate products — and the edition axis TCGplayer
// prices as separate skus of one product (1st Edition, Unlimited, Limited)
// is separate entries too, each with its own id, priced by construction —
// the editions-as-flags shape this datastore used to publish folded those
// price points onto one id. The id's edition suffix derives from the
// printing name alone, never from which sibling printings exist, so an id
// cannot churn when TCGplayer later adds an edition to a product.
//
// The name parentheticals TCGplayer decorates products with are told
// apart per collector number, the same way cmd/onepiece does: a
// parenthetical every product of a number carries is part of the card's
// name (Speed Duel deck letters, the alternate arts that are the number's
// whole identity); a qualifier that merely restates the product's own
// Rarity — spelled out, shorthanded ("UTR"), or with "Rare" elided
// ("Starfoil") — is dropped as redundant with the rarity field; whatever
// remains is the variant label the matcher narrows on.
//
// Sets are the catalog groups, kept separate per print run (LOB and its
// 25th Anniversary reissue are different sets). Group abbreviations
// repeat and are sometimes blank, but set codes must be unique, so a
// blank abbreviation gets a code minted from the group id and a repeated
// one gets the group id suffixed onto the later group, both logged.
//
// YGOPRODeck contributes only what the catalog lacks: groups TCGplayer
// has no release date for carry the request time as publishedOn instead,
// and those get the date YGOPRODeck knows, joined by abbreviation first
// and set name second, only when the join is unambiguous. cardinfo.php is
// deliberately not fetched and no YGOPRODeck image URL is stored: their
// terms forbid hotlinking, so images are TCGplayer's alone.
//
// Every product the catalog types as a card becomes an entry, and validate
// refuses a build that left one out: a shape nobody has seen yet stops the
// publish instead of vanishing from the datastore. The products the game
// gives no collector number — the field-center tokens, the art and divider
// cards, the oversized promos — are carried on the id their product alone
// mints, the same shape cmd/pokemon files its basic energies under, and
// they are told apart by the set, the rarity and the variant label the
// product name spells out.
//
// Sealed products are everything the catalog files outside the singles
// type, same as the other games: by exclusion, so a product type
// TCGplayer adds later lands on the sealed side where it is noticed.
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
	yugiohCategory = 2

	ygoprodeckSetsURL  = "https://db.ygoprodeck.com/api/v7/cardsets.php"
	ygoprodeckCardsURL = "https://db.ygoprodeck.com/api/v7/cardinfo.php"
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; everything else is sealed by exclusion.
var tcgSingles = tcgplayer.SinglesProductTypes(yugiohCategory)

// finishSuffix maps each sku printing name to the suffix its entry's id
// carries. Any other printing name is a hard failure, because a suffix
// invented on the fly would not be a stable identity — the category's
// fourth printing, Normal, appears only on sealed skus and stays out.
var finishSuffix = map[string]string{
	"1st Edition": "_1e",
	"Unlimited":   "_unl",
	"Limited":     "_lim",
}

// finishOrder fixes the order a product's entries are emitted in.
var finishOrder = []string{
	"1st Edition",
	"Unlimited",
	"Limited",
}

// hasDate reports whether the group's publishedOn is a real date: the
// catalog stamps the request time on groups it has no date for, so a
// genuine value is always a bare midnight timestamp.
func hasDate(g tcgplayer.Group) bool {
	return strings.HasSuffix(g.PublishedOn, "T00:00:00")
}

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the distinct printing names its skus
// carry, in finishOrder; a printing the catalog does not list for a product
// is one that does not exist.

// ygoSet is the slice of a YGOPRODeck cardsets entry this build reads.
type ygoSet struct {
	Name string `json:"set_name"`
	Code string `json:"set_code"`
	Date string `json:"tcg_date"`
}

// ygoCard is the slice of a YGOPRODeck card this build reads: Konami's own
// passcode, and the printings it names by collector number. No image is
// read or stored - YGOPRODeck's terms forbid hotlinking theirs, and the
// catalog's own are what the datastore carries.
type ygoCard struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Sets []struct {
		Code   string `json:"set_code"`
		Rarity string `json:"set_rarity"`
	} `json:"card_sets"`
}

// konamiIDs maps a collector number to the passcode of the card printed
// under it, and where a number names several cards - the same code reused
// across a set's rarities is one card, but a handful of numbers upstream
// spells two ways - to the passcode its rarity picks out.
//
// The passcode is Konami's own identifier for a card, the one every other
// Yu-Gi-Oh source keys on, and the datastore carried nothing but a
// TCGplayer product id until now: a listing naming a passcode had no way
// in, and no printing could be checked against what upstream says is
// printed under its number.
func konamiIDs(cards []ygoCard) (map[string]int, map[string]int) {
	byNumber := map[string]map[int]bool{}
	byNumberRarity := map[string]map[int]bool{}
	for _, card := range cards {
		if card.ID == 0 {
			continue
		}
		for _, set := range card.Sets {
			code := strings.ToUpper(strings.TrimSpace(set.Code))
			if code == "" {
				continue
			}
			if byNumber[code] == nil {
				byNumber[code] = map[int]bool{}
			}
			byNumber[code][card.ID] = true

			key := code + "|" + normRarity(set.Rarity)
			if byNumberRarity[key] == nil {
				byNumberRarity[key] = map[int]bool{}
			}
			byNumberRarity[key][card.ID] = true
		}
	}
	only := func(in map[string]map[int]bool) map[string]int {
		out := map[string]int{}
		for key, ids := range in {
			if len(ids) != 1 {
				continue
			}
			for id := range ids {
				out[key] = id
			}
		}
		return out
	}
	return only(byNumber), only(byNumberRarity)
}

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

// idBase mints the id stem an entry's edition suffix hangs off: the
// collector number and the product id, or the product id alone for a
// product the game gives no number.
func idBase(num string, productID int) string {
	if num == "" {
		return strconv.Itoa(productID)
	}
	return strings.ToLower(num) + "_" + strconv.Itoa(productID)
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

// normalizeName reduces a set name to what two spellings share, so the
// catalog's "Labyrinth_of_Nightmare" still finds YGOPRODeck's "Labyrinth
// of Nightmare".
func normalizeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rarityShorthand spells out the abbreviations TCGplayer decorates
// product names with; each is honored only when the expansion matches the
// product's own Rarity, so a wrong or drifting entry cannot misfold.
var rarityShorthand = map[string]string{
	"UR":  "Ultra Rare",
	"UTR": "Ultimate Rare",
	"SR":  "Super Rare",
	"CR":  "Collector's Rare",
	"PSR": "Prismatic Secret Rare",
	"PUR": "Prismatic Ultimate Rare",
	"PCR": "Prismatic Collector's Rare",
	"EUR": "Emblazoned Ultra Rare",
	"ESR": "Emblazoned Secret Rare",
}

// rarityOf is the rarity a product is filed under, with the padding
// TCGplayer's own data entry leaves on it taken off. The category's rarity
// table spells one of them "Prismatic Collector's Rare " with a trailing
// space while the products carry it without, and the two have to agree:
// the rarity is part of the identity a query resolves a Yu-Gi-Oh card by -
// the axis this game varies on - so a padded one is a card nothing can ask
// for and, worse, a second identity for a card that already has one.
func rarityOf(product tcgplayer.Product) string {
	return strings.TrimSpace(product.Extended("Rarity"))
}

// normRarity folds the case, spacing and apostrophe variants two
// spellings of the same rarity differ by.
func normRarity(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(s, "’", "'"))), " ")
}

// restatesRarity reports whether a name qualifier merely repeats the
// product's Rarity: spelled out, shorthanded, or with the trailing
// "Rare" elided ("Starfoil", "Secret").
func restatesRarity(qual, rarity string) bool {
	r := normRarity(rarity)
	if r == "" {
		return false
	}
	q := normRarity(qual)
	return q == r || q+" rare" == r || normRarity(rarityShorthand[qual]) == r
}

// handDates are release dates for the groups neither source can date: the
// catalog stamps its request time on them and YGOPRODeck has no set to
// join. Each is researched, not guessed, and keyed by the group id, which
// the catalog never reuses. A group holding several years of one program
// carries the day the program began, so it sorts where its oldest cards
// belong.
var handDates = map[int]string{
	// Pharaoh Tour promotional cards: PT1 released December 17, 2005 in
	// Europe (Yugipedia, "Pharaoh Tour 2005 promotional cards"); the
	// group also holds the later PT02 and PT03 tours.
	2215: "2005-12-17",
	// The Lost Art Promotion opened with Monster Reborn in North America
	// on February 1, 2018 (Yugipedia, "The Lost Art Promotion A").
	2196: "2018-02-01",
	// The World Championship Japanese Promotional Packs begin with the
	// 2017-JPP cards handed out at Worlds 2017, held in Tokyo on August
	// 12-13, 2017; the group also holds the 2018 and 2019 packs.
	2353: "2017-08-12",
	// The video-game promotional cards begin with Dark Duel Stories
	// (DDS), whose cards released March 19, 2002 in North America
	// (Yugipedia, "Yu-Gi-Oh! Dark Duel Stories promotional cards").
	1921: "2002-03-19",

	// The six below are empty legacy groups - TCGplayer files no product
	// under them today - but each names a real release, so they carry its
	// day: the set sorts where it belongs, and the date is already right
	// should TCGplayer ever file a product there. North American dates
	// throughout, matching the English catalog the rest of the datastore
	// is built from.
	//
	// The Duelist Genesis, released September 2, 2008 (Yugipedia).
	181: "2008-09-02",
	// Forbidden Legacy, the FL1 Special Edition blister of the game's
	// first three booster sets, released October 1, 2005 (Yugipedia).
	206: "2005-10-01",
	// The Sacred Cards, whose TSC promos shipped with the game's North
	// American release on November 4, 2003 (Yugipedia).
	331: "2003-11-04",
	// The 2009 Collectible Tins (the CT06 promos), wave 1 released
	// August 18, 2009 (Yugipedia, "Collectible Tins 2009 Wave 1").
	1301: "2009-08-18",
	// Yu-Gi-Oh! 5D's Tag Force 4, whose promos shipped with the game's
	// North American release on November 17, 2009 (Yugipedia).
	1318: "2009-11-17",
	// The Lost Millennium: Special Edition, released June 10, 2005
	// (Yugipedia).
	1342: "2005-06-10",

	// Jump Award (group 240) stays undated on purpose: it is empty, no
	// source resolves what it ever named, and every plausible match (the
	// Shonen Jump magazine promos, the SJC prize cards) already has a
	// group of its own. A date here would be a guess wearing a citation -
	// and while the group stays empty it emits no set anyway.
}

// handNames correct the names the catalog transcribed short, keyed by the
// product id, which the catalog never reuses. Only a character the catalog
// could not carry belongs here - not a card Konami later renamed. The
// catalog's "Red-Eyes B. Dragon" is what that card's early printings say
// on them, and the datastore keeps it: a storefront selling one writes the
// printed name, and the passcode already joins it to whatever Konami calls
// the card today.
//
// The test that a name belongs here is a sibling printing of the same
// passcode spelling it in full. 270 entries name their card differently
// from YGOPRODeck and all but this one are that other thing - a rename, a
// storefront's disambiguating suffix - so the table is one line rather
// than a rule.
var handNames = map[int]string{
	// Kuwagata α, the only card in the game whose name carries a Greek
	// letter. Tournament Pack 1 dropped it and left "Kuwagata"; OTS
	// Tournament Pack 19 writes the same passcode (60802233) out as
	// "Kuwagata Alpha", and that spelling is the one carried here - the
	// letter itself normalizes to a third key nothing else in the
	// datastore uses, and no other entry spells a Greek letter at all.
	22721: "Kuwagata Alpha",
}

// treatments are the qualifiers that name how a printing was made rather
// than which card it is, and so may never be elected into a name however
// many printings of a number carry them.
//
// The election folds a qualifier every printing of a number carries into
// the card's name, because it tells none of them apart - the rarity does.
// That reads right for a number and wrong for a card: the seven printings
// of RA04-EN024 are all "Alternate Art", so all seven were named "Aleister
// the Invoker (Alternate Art)", which is a card no storefront sells and no
// search for "Aleister the Invoker" finds. The deck letters and the monster
// a Field Center Token shows are genuinely part of a name and stay elected;
// a treatment becomes the variant label it always was, and the rarity goes
// on telling the printings apart, which is what it was already doing.
var treatments = map[string]bool{
	"alternate art": true,
}

func isTreatment(qualifier string) bool {
	return treatments[strings.ToLower(strings.TrimSpace(qualifier))]
}

// isPromoGroup reports whether a catalog group hands out promotional
// printings. The group name is the only thing that says so in this
// category: Yu-Gi-Oh rarities name the foil treatment ("Secret Rare",
// "Starfoil Rare") and never the promotion, so the 732 Duelist League
// promos carry no promotional rarity at all. Reading the name also keeps
// the collector tins out, which reprint at retail rather than hand out.
func isPromoGroup(group tcgplayer.Group) bool {
	return strings.Contains(strings.ToLower(group.Name), "promo")
}

// lowered folds a label list to the spelling the matcher declares tags in.
func lowered(quals []string) []string {
	out := make([]string, len(quals))
	for i, q := range quals {
		out[i] = strings.ToLower(q)
	}
	return out
}

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)
var bareNumRe = regexp.MustCompile(`^\d{1,4}$`)

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []string
}

// decompose strips the collector number worn as decoration (a dash
// suffix, a parenthetical repeat, a bare numeric parenthetical) and the
// qualifiers that only restate the product's Rarity, keeping the rest for
// the name-versus-variant call made per collector number below.
func decompose(p tcgplayer.Product, num string) single {
	name := p.Name
	name = strings.ReplaceAll(name, " - "+num, "")
	rarity := rarityOf(p)

	var quals []string
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if bareNumRe.MatchString(q) || strings.EqualFold(q, num) || restatesRarity(q, rarity) {
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

func printingNames(c *tcgplayer.CatalogDump) map[int][]string {
	name := map[int]string{}
	for _, p := range c.Printings {
		name[p.PrintingID] = p.Name
	}

	rank := map[string]int{}
	for i, n := range finishOrder {
		rank[n] = i
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
		sort.Slice(names, func(i, j int) bool {
			ri, iKnown := rank[names[i]]
			rj, jKnown := rank[names[j]]
			if iKnown && jKnown {
				return ri < rj
			}
			if iKnown != jKnown {
				return iKnown
			}
			return names[i] < names[j]
		})
		out[product.ProductID] = names
	}
	return out
}

// nonCodeRe matches the runs a set code cannot carry.
var nonCodeRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// setCodeOf reduces a catalog abbreviation to what a search query can carry.
// A set code is typed after "is:", and a query is split on whitespace before
// a filter ever sees it and on the colon that names the filter, so a code
// holding either cannot be asked for: "is:OP11 RE" reaches the filter as
// "is:OP11" and "is:crz:gg" names a filter called crz. Every run of anything
// but a letter or a digit becomes one dash, and the ends are trimmed of them.
func setCodeOf(abbreviation string) string {
	return strings.Trim(nonCodeRe.ReplaceAllString(abbreviation, "-"), "-")
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
		Sealed []json.RawMessage `json:"sealed"`
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
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 2 (required)")
	ygoSets := flag.String("ygoprodeck-sets", ygoprodeckSetsURL, "YGOPRODeck cardsets file, path or URL")
	ygoCards := flag.String("ygoprodeck-cards", ygoprodeckCardsURL, "YGOPRODeck cardinfo file, path or URL")
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
	if catalog.Category.CategoryID != yugiohCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, yugiohCategory)
	}

	setsData, err := fetch(*ygoSets)
	if err != nil {
		log.Fatalln("ygoprodeck sets:", err)
	}
	var ygo []ygoSet
	if err := json.Unmarshal(setsData, &ygo); err != nil {
		log.Fatalln("ygoprodeck sets:", err)
	}
	// Index the dates by code and by normalized name. Several YGOPRODeck
	// entries share a code (a set beside its special editions), so a key
	// maps to every distinct date it was seen with, and only a key with
	// exactly one is trusted for a fill.
	datesByCode := map[string][]string{}
	datesByName := map[string][]string{}
	addDate := func(index map[string][]string, key, date string) {
		if key == "" || sliceContains(index[key], date) {
			return
		}
		index[key] = append(index[key], date)
	}
	// The name YGOPRODeck files a code under, for the sets this build has
	// to name itself. A code several sets share names none of them, the
	// way a shared date dates none of them.
	namesByCode := map[string]string{}
	ambiguous := map[string]bool{}
	for _, set := range ygo {
		code := strings.ToUpper(set.Code)
		if have, seen := namesByCode[code]; seen && have != set.Name {
			ambiguous[code] = true
		}
		namesByCode[code] = set.Name
	}
	for code := range ambiguous {
		delete(namesByCode, code)
	}
	for _, set := range ygo {
		if set.Date == "" {
			continue
		}
		addDate(datesByCode, strings.ToUpper(set.Code), set.Date)
		addDate(datesByName, normalizeName(set.Name), set.Date)
	}
	log.Printf("catalog: %d groups, %d products; ygoprodeck: %d sets over %d codes",
		len(catalog.Groups), len(catalog.Products), len(ygo), len(datesByCode))

	// Konami's passcodes, joined onto the printings by collector number.
	// A source that will not answer costs the annotation and nothing else:
	// the datastore is the catalog's, and the passcode rides along on it.
	var passcodeByNumber, passcodeByNumberRarity map[string]int
	cardsData, err := fetch(*ygoCards)
	if err != nil {
		log.Printf("ygoprodeck cards: %v (passcodes not annotated)", err)
	} else {
		var payload struct {
			Data []ygoCard `json:"data"`
		}
		if err := json.Unmarshal(cardsData, &payload); err != nil {
			log.Printf("ygoprodeck cards: %v (passcodes not annotated)", err)
		} else {
			passcodeByNumber, passcodeByNumberRarity = konamiIDs(payload.Data)
			log.Printf("ygoprodeck cards: %d cards, %d collector numbers naming one passcode",
				len(payload.Data), len(passcodeByNumber))
		}
	}

	// lookup finds the YGOPRODeck dates for a group: abbreviation match
	// first (whole, then the prefix ahead of the language tail "LOB-EN"
	// carries), set name second.
	lookup := func(g tcgplayer.Group) (dates []string, how string) {
		abbr := strings.ToUpper(g.Abbreviation)
		if abbr != "" {
			dates = datesByCode[abbr]
			if dates == nil {
				dates = datesByCode[strings.SplitN(abbr, "-", 2)[0]]
			}
			if dates != nil {
				return dates, "code"
			}
		}
		dates = datesByName[normalizeName(g.Name)]
		if dates != nil {
			return dates, "name"
		}
		return nil, ""
	}

	// Assign every group its set code, in group-id order so the original
	// print run keeps the bare abbreviation and the reissue gets marked. A
	// blank abbreviation gets a code minted from the group id.
	groups := append([]tcgplayer.Group(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GroupID < groups[j].GroupID
	})
	// A group holding nothing at all emits no set, so it takes no code
	// either. It used to take one on the way past and then leave, on the
	// reasoning that a code held is a code that will not move the day
	// TCGplayer files a product there - but an empty group is a set no
	// consumer can see, and holding a code for it costs a set that is
	// there: an empty group abbreviated 5DS1 sent the real 5D's starter
	// deck to "5DS1-139", where its own cards' numbers cannot reach it.
	// A set nobody can see does not get to name one that everybody can.
	//
	// The test is any product, not any card: a group of nothing but
	// sealed emits a set too, and a set is not published without a code.
	carded := map[int]bool{}
	for _, product := range catalog.Products {
		carded[product.GroupID] = true
	}

	// The prefix a group's cards' own numbers open with. A collector
	// number opens with its set's code - LOB-EN001 is LOB - and a
	// storefront filing a shelf under one catch-all edition leaves that
	// prefix as the only thing saying which set a listing belongs to. So
	// where a group's cards agree on a prefix, and no other group's cards
	// use it, the prefix is what the set is called, whatever the catalog
	// abbreviated it as.
	prefixes := map[int]map[string]bool{}
	owners := map[string]map[int]bool{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		prefix, _, found := strings.Cut(strings.ToUpper(product.Extended("Number")), "-")
		if !found || prefix == "" {
			continue
		}
		if prefixes[product.GroupID] == nil {
			prefixes[product.GroupID] = map[string]bool{}
		}
		prefixes[product.GroupID][prefix] = true
		if owners[prefix] == nil {
			owners[prefix] = map[int]bool{}
		}
		owners[prefix][product.GroupID] = true
	}
	// The one prefix a group speaks with, empty where it speaks with
	// several or shares one. Two editions of a set print one number space
	// - Metal Raiders beside its 25th Anniversary reissue - and neither
	// may take the space's name from the other.
	soleprefix := func(groupID int) string {
		if len(prefixes[groupID]) != 1 {
			return ""
		}
		var prefix string
		for p := range prefixes[groupID] {
			prefix = p
		}
		if len(owners[prefix]) != 1 {
			return ""
		}
		return prefix
	}

	setCodes := map[int]string{}
	usedCodes := map[string]bool{}
	var minted, suffixed, renamed int
	// The abbreviations first, so a prefix is only adopted where it is
	// not already some set's name.
	for _, group := range groups {
		if carded[group.GroupID] {
			usedCodes[strings.ToUpper(setCodeOf(group.Abbreviation))] = true
		}
	}
	claimed := usedCodes
	usedCodes = map[string]bool{}
	for _, group := range groups {
		if !carded[group.GroupID] {
			continue
		}
		code := setCodeOf(group.Abbreviation)
		if prefix := soleprefix(group.GroupID); prefix != "" &&
			!strings.EqualFold(prefix, code) && !claimed[prefix] {
			log.Printf("%s: abbreviated %q, but every card is numbered %s-; set code %s",
				group.Name, group.Abbreviation, prefix, prefix)
			code = prefix
			renamed++
		}
		if code == "" {
			code = fmt.Sprintf("G%d", group.GroupID)
			minted++
			log.Printf("%s: no abbreviation, set code %s minted", group.Name, code)
		}
		if usedCodes[code] {
			code = fmt.Sprintf("%s-%d", code, group.GroupID)
			suffixed++
			log.Printf("%s: abbreviation %s already taken, set code %s minted",
				group.Name, group.Abbreviation, code)
		}
		if usedCodes[code] {
			log.Fatalf("set code %s still not unique; refusing to guess further", code)
		}
		usedCodes[code] = true
		setCodes[group.GroupID] = code
	}
	log.Printf("set codes: %d taken from the cards' own numbers, %d minted for blank abbreviations, %d deduplicated",
		renamed, minted, suffixed)

	// A group whose cards speak with several prefixes is several sets the
	// catalog files on one shelf. "Duelist League Promo" holds 732 cards
	// numbered DL09-, DL11-, DL13- and six more: nine Duelist League
	// seasons, each its own set everywhere but here, and none of them
	// reachable by its number because the shelf is named for none of them.
	//
	// So the cards go to sets of their own, one per prefix. The same three
	// tests the renaming above applies decide it - the prefix names no set
	// already, and no other group's cards use it - because a split that
	// took a name in use would be the collision the suffixing exists to
	// avoid, in a new place.
	//
	// What is not numbered stays where it was. A shelf of tokens holds 90
	// cards with no number beside 39 that have one, and the 90 have said
	// nothing about where they belong; so does the sealed, which the
	// catalog files by group and this build has no finer answer for.
	splitCodes := map[int]map[string]string{}
	claimedCode := map[string]bool{}
	for _, code := range setCodes {
		claimedCode[strings.ToUpper(code)] = true
	}
	var splitGroups, splitSets int
	for _, group := range groups {
		if len(prefixes[group.GroupID]) < 2 {
			continue
		}
		free := true
		for prefix := range prefixes[group.GroupID] {
			if claimedCode[prefix] || len(owners[prefix]) != 1 {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		split := map[string]string{}
		var names []string
		for prefix := range prefixes[group.GroupID] {
			split[prefix] = prefix
			claimedCode[prefix] = true
			names = append(names, prefix)
		}
		sort.Strings(names)
		splitCodes[group.GroupID] = split
		splitGroups++
		splitSets += len(split)
		log.Printf("%s: its cards are numbered %s; filed as %d sets rather than one",
			group.Name, strings.Join(names, ", "), len(split))
	}
	if splitGroups > 0 {
		log.Printf("set splits: %d groups holding several sets' cards became %d sets",
			splitGroups, splitSets)
	}

	// A card's set is its own number's, where the shelf it sits on holds
	// several sets' worth.
	cardSet := func(s single) string {
		if split := splitCodes[s.product.GroupID]; split != nil {
			if prefix, _, found := strings.Cut(strings.ToUpper(s.number), "-"); found {
				if code, ok := split[prefix]; ok {
					return code
				}
			}
		}
		return setCodes[s.product.GroupID]
	}

	// Resolve every group's release date: publishedOn is authoritative
	// when real, the unambiguous YGOPRODeck date fills the placeholders,
	// and what neither source can date stays empty rather than guessed.
	// A group with no product is a legacy husk TCGplayer keeps around; it
	// emits no set below, so it is not worth dating, joining, or being
	// reported as undated - the empty groups were most of what the
	// left-undated log named.
	productsIn := map[int]int{}
	for _, product := range catalog.Products {
		productsIn[product.GroupID]++
	}

	releaseDates := map[int]string{}
	var joinedByCode, joinedByName, placeholders, filled, unfilled int
	for _, group := range groups {
		if productsIn[group.GroupID] == 0 {
			continue
		}
		dates, how := lookup(group)
		switch how {
		case "code":
			joinedByCode++
		case "name":
			joinedByName++
		}
		if hasDate(group) {
			releaseDates[group.GroupID] = group.ReleaseDate()
			continue
		}
		placeholders++
		if len(dates) == 1 {
			releaseDates[group.GroupID] = dates[0]
			filled++
			log.Printf("%s (%s): release date %s filled from ygoprodeck by %s",
				group.Name, setCodes[group.GroupID], dates[0], how)
			continue
		}
		if date, found := handDates[group.GroupID]; found {
			releaseDates[group.GroupID] = date
			filled++
			log.Printf("%s (%s): release date %s filled by hand",
				group.Name, setCodes[group.GroupID], date)
			continue
		}
		unfilled++
		log.Printf("%s (%s): no release date and %d ygoprodeck candidates, left undated",
			group.Name, setCodes[group.GroupID], len(dates))
	}
	log.Printf("ygoprodeck join: %d of %d groups (%d by code, %d by name); %d placeholder dates, %d filled, %d left empty",
		joinedByCode+joinedByName, len(groups), joinedByCode, joinedByName,
		placeholders, filled, unfilled)

	printings := printingNames(&catalog)

	// Split the products: every single becomes card entries, the
	// non-single types become sealed. "N/A" is the catalog's spelling for
	// a product with no number, and a product with none is still a card.
	var singles []single
	var sealedProducts []tcgplayer.Product
	var unnumbered int
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			// Every card product the catalog has ever carried prices at
			// least one sku, and a product with none has no printing to
			// file an entry under: stop rather than drop it.
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
	log.Printf("singles: %d kept (%d without a collector number)", len(singles), unnumbered)
	var unrated int
	for _, s := range singles {
		if rarityOf(s.product) == "" {
			unrated++
			log.Printf("no rarity: %q (%d) carried without one", s.product.Name, s.product.ProductID)
		}
	}
	if unrated > 0 {
		log.Printf("no rarity: %d products, carried and logged rather than refused", unrated)
	}
	if len(singles) == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}

	// Per collector number within its group: a qualifier every product of
	// the number carries is part of the name, not a variant. A number with
	// a single product cannot make that call alone, so the name parts
	// learned from the multi-product numbers decide for it — the same
	// deck letter or epithet decorates the number's every printing.
	byNumber := map[string][]*single{}
	for i := range singles {
		// The unnumbered products of a group are unrelated cards, not one
		// card's printings, so they elect nothing together: they take the
		// verdicts the real numbers reached, as a lone printing does.
		if singles[i].number == "" {
			continue
		}
		key := fmt.Sprintf("%d|%s", singles[i].product.GroupID, singles[i].number)
		byNumber[key] = append(byNumber[key], &singles[i])
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
			if n == len(bucket) && !isTreatment(q) {
				nameParens[q] = true
			}
		}
	}
	assemble := func(s *single, isName map[string]bool) {
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
				isName[q] = n == len(bucket) && !isTreatment(q)
			}
		}
		for _, s := range bucket {
			assemble(s, isName)
		}
	}
	for i := range singles {
		if singles[i].number != "" {
			continue
		}
		assemble(&singles[i], nameParens)
	}

	// Emit. Sets are the catalog groups that hold anything; ids embed the
	// product id so they survive any upstream renumbering. An empty group
	// is skipped and, as above, holds no code while it is empty.
	// What still answers to a code. A shelf whose cards went to sets of
	// their own keeps its own set only for what did not move - its
	// unnumbered cards and its sealed - and drops it where nothing did.
	carried := map[string]bool{}
	for _, s := range singles {
		carried[cardSet(s)] = true
	}
	for _, product := range sealedProducts {
		carried[setCodes[product.GroupID]] = true
	}
	sets := map[string]any{}
	var promoSets, skippedEmpty, splitNamed, splitUnnamed int
	for _, group := range groups {
		if productsIn[group.GroupID] == 0 {
			skippedEmpty++
			continue
		}
		add := func(code, name, date string) {
			set := map[string]any{"name": name, "releaseDate": date}
			if isPromoGroup(group) {
				set["type"] = "promo"
				promoSets++
			}
			sets[code] = set
		}
		if carried[setCodes[group.GroupID]] {
			add(setCodes[group.GroupID], group.Name, releaseDates[group.GroupID])
		}
		// A set split off the shelf is named and dated by its own code
		// where YGOPRODeck knows it, which is what the shelf's one name
		// and one date cannot do for nine Duelist League seasons. Where
		// it does not, the shelf's name qualified by the code says as
		// much as this build can honestly say.
		for prefix, code := range splitCodes[group.GroupID] {
			if !carried[code] {
				continue
			}
			name, known := namesByCode[strings.ToUpper(prefix)]
			if known {
				splitNamed++
			} else {
				name = group.Name + " (" + prefix + ")"
				splitUnnamed++
			}
			date := releaseDates[group.GroupID]
			if own := datesByCode[strings.ToUpper(prefix)]; len(own) == 1 {
				date = own[0]
			}
			add(code, name, date)
		}
	}
	if splitNamed+splitUnnamed > 0 {
		log.Printf("split sets: %d named by their own code, %d kept the shelf's name",
			splitNamed, splitUnnamed)
	}
	if skippedEmpty > 0 {
		log.Printf("sets: %d empty groups hold no product and are skipped", skippedEmpty)
	}
	log.Printf("promotional sets: %d of %d", promoSets, len(sets))

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
	var passcoded int
	for _, s := range singles {
		cardType := s.product.Extended("Card Type")
		if cardType == "" {
			cardType = s.product.Extended("MonsterType")
		}
		productID := s.product.ProductID
		for _, finish := range printings[productID] {
			suffix, known := finishSuffix[finish]
			if !known {
				log.Fatalf("product %d carries printing %q, not one of the three this identity scheme knows",
					productID, finish)
			}
			links := map[string]any{"tcgPlayerId": productID}
			// The passcode the collector number names, and where the
			// number names several cards, the one its rarity picks out.
			if s.number != "" {
				number := strings.ToUpper(s.number)
				passcode, found := passcodeByNumber[number]
				if !found {
					passcode, found = passcodeByNumberRarity[number+"|"+normRarity(rarityOf(s.product))]
				}
				if found {
					links["konamiId"] = passcode
					passcoded++
				}
			}
			name := s.baseName
			if corrected, hand := handNames[productID]; hand {
				name = corrected
			}
			entry := map[string]any{
				"id":        idBase(s.number, productID) + suffix,
				"name":      name,
				"setCode":   cardSet(s),
				"rarity":    rarityOf(s.product),
				"attribute": s.product.Extended("Attribute"),
				"type":      cardType,
				"finish":    finish,
				"image":     imageURL(s.product.ImageURL),

				"externalLinks": links,
			}
			if s.number != "" {
				entry["number"] = s.number
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
				// The same labels as a list: joined, "OTS Stamp Blue"
				// cannot be read back into the two tags it holds, and the
				// matcher needs them whole to declare and to match on.
				entry["promoTypes"] = lowered(s.quals)
			}
			cards = append(cards, entry)
		}
	}

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []any
	for _, product := range sealedProducts {
		code := setCodes[product.GroupID]
		sealed = append(sealed, map[string]any{
			"id":          fmt.Sprintf("%s-%d", strings.ToLower(code), product.ProductID),
			"name":        product.Name,
			"setCode":     code,
			"releaseDate": releaseDates[product.GroupID],
			"image":       imageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	log.Printf("konami passcodes: %d of %d entries annotated", passcoded, len(cards))
	log.Printf("emitting %d sets, %d card entries over %d products, %d sealed",
		len(sets), len(cards), len(singles), len(sealed))
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	doc := map[string]any{
		"game":   "yugioh",
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
	// publishing anything: a format drift or a truncated download must
	// fail here, not in every consumer. The types mirror what go-mtgban's
	// mtgmatcher/yugioh reads, duplicated so this repository depends on
	// nothing.
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
	// the products would leave the datastore with nothing to say so, the
	// card side's invariant being blind to them. Counting the emitted
	// products back against the catalog total is what says so.
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
			// Measuring from the high-water mark instead means the drift
			// has to stay under the tolerance in total, not per night.
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
		_, found := got[productID]
		if !found {
			missing = append(missing, productID)
		}
	}
	for productID := range got {
		_, found := want[productID]
		if !found {
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

// validate decodes an encoded datastore and checks its shape: every card
// and sealed product carrying its identity — for a card that includes the
// rarity it is varied by and the edition its skus price — every id unique
// within its namespace, no two products wearing the same identity, every
// referenced set existing, every finish one of the three printing names,
// and every product's entries covering exactly the sku printings the
// catalog lists for it.
// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// idShape is what a uuid has to look like wherever one is written down: a
// slash is a path separator and a space ends a word, and a uuid travels
// through urls, filenames and query strings alike.
var idShape = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

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

	if doc.Game != "yugioh" {
		return out, fmt.Errorf("game is %q, not yugioh", doc.Game)
	}
	for code, set := range doc.Sets {
		if code == "" || set.Name == "" {
			return out, fmt.Errorf("set %q missing its identity", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cardIDs := map[string]bool{}
	// A query resolves a card by its name, number, set, rarity and variant
	// label, never by the id, so two products wearing all five alike are one
	// card to every consumer and would alias each other's prices. Rarity is
	// in the key because it is the axis this game varies on: a number is
	// reprinted across rarities as separate products carrying one name and
	// no variant label of their own, and the matcher narrows on the rarity
	// to tell them apart, so the four axes the other games identify by would
	// call thousands of those reprints one card. The key holds the product
	// id rather than a flag so a product's own edition entries pass while
	// two different products never do - keying on the finish instead would
	// wave through exactly the pair this is meant to catch, since most
	// products carry a single edition.
	identities := map[string]int{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		// The rarity is this game's variant axis and part of the identity,
		// but its presence is TCGplayer's to provide, not this build's to
		// demand: a freshly listed product carries none for a day, and one
		// card without a rarity is no reason to publish nothing at all.
		// The identity check below still refuses two products that are
		// indistinguishable without it.
		if card.ID == "" || card.Name == "" ||
			card.Finish == "" || card.ExternalLinks.TcgPlayerId == 0 {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q", card.Name, card.ID, card.Number)
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
	err := coverage(gotFinishes, wantFinishes)
	if err != nil {
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
	"\u2018", "'", "\u2019", "'", "\u201c", `"`, "\u201d", `"`)

// plainQuotes rewrites those quotes wherever the document carries them.
// The catalogs are not consistent about it: TCGplayer sells "Rocket's
// Hitmonchan" with a curly apostrophe beside hundreds of names holding a
// plain one, files two Yu-Gi-Oh rarities as "Ultra Pharaoh's Rare" while
// every other name uses ASCII, and spells one One Piece card
// Eustass"Captain"Kid on the card and Eustass"Captain"Kid on the box it
// comes in. A query carries one spelling, so the card filed under the
// other cannot be found, and the two rarities cannot be asked for at all.
//
// The whole document is walked rather than the fields known to carry
// them, because the field that starts carrying them tomorrow would
// otherwise be missed, and it runs before the output is encoded so the
// check that re-reads it sees exactly what will be published.
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
