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
//     sub-types, and lets the catalog settle which of them exist.
//
// The catalog decides which finishes a card has; upstream decides what the
// foils are called. A printing TCGplayer prices a sku for is one that
// exists - it is selling it - and upstream saying otherwise cost the card
// its uuid, the loader building them from foilTypes: a card upstream calls
// foil-only got no nonfoil uuid, so every nonfoil listing of it resolved to
// nothing while the shop sold it. Upstream keeps naming the foils, because
// its sub-types ("Silver", "Tempest", "RainbowPillars") are what
// mtgmatcher/lorcana's selectFinish resolves storefront wording against and
// TCGplayer, knowing only Normal, Holofoil and Cold Foil, can reproduce
// none of them.
//
// The promotional printings TCGplayer files in their own groups (DLPC, D23,
// D100) are matched onto upstream's own cards wherever the id fill above
// can do it by name and number, because upstream files them under the set
// they belong to and its card is the better one. What no card claims or
// matches is minted here rather than dropped: a product TCGplayer sells is
// a printing that exists, and a datastore leaving it out leaves every
// listing of it unresolvable.
//
// A minted card is filed under the negated product id. LorcanaJSON's ids
// are positive counting numbers, so the negative half of the integer space
// is unmistakably ours and cannot collide with an id upstream publishes
// later however far its numbering runs — which is what kept these products
// out before — and the product a card was minted from reads straight off
// its id. Everything else a minted card carries is the catalog's own word:
// the product name, the group's set code — its abbreviation, or the
// abbreviation with the group id suffixed where an earlier group already
// claimed it, so two groups can never fold onto one set — the collector
// number where there is one and 0 where there is none, the rarity, the
// printings as foil types, and the language for a printing sold in no
// English sku. The day upstream publishes the real card, its own entry
// claims the product id and the minted one stops being minted.
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
	"unicode"
)

const (
	// lorcanaCategory is Lorcana's TCGplayer category, the one the catalog
	// dump is expected to carry.
	lorcanaCategory = 71

	// englishLanguage is the catalog's language id for English, the one a
	// product needs a sku in to be part of the English program.
	englishLanguage = 1
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game. Everything else the catalog carries is
// a sealed product: the comparison is against the singles types rather
// than a list of sealed ones, so a type TCGplayer adds later lands on the
// sealed side where it is noticed instead of silently passing as a single.
var tcgSingles = tcgplayer.SinglesProductTypes(lorcanaCategory)

// releaseDate reduces a group's publishedOn timestamp to the bare day
// LorcanaJSON dates carry ("2023-08-18T00:00:00" -> "2023-08-18").

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the sorted printing names it is sold
// under. TCGplayer's category 71 has exactly three — Normal, Holofoil and
// Cold Foil — and a printing it does not list for a product is one that
// does not exist.
func printingNames(c *tcgplayer.CatalogDump) map[int][]string {
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
		if canonicalFinish(name) == finishNonfoil {
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

// setCodes assigns every catalog group the set code its minted cards and
// sealed products are filed under: its own abbreviation, which is what
// LorcanaJSON calls the set where upstream carries one, and the
// abbreviation with the group id suffixed where an earlier group already
// claimed it. Abbreviations repeat across groups in every other category,
// and a second group filed under a code the first already holds had its
// name, its date and its whole identity folded onto that first group's set
// - silently, because the code still resolved for every card naming it.
// Codes are claimed in group-id order, so the group that claimed one keeps
// it bare and only the later arrival is marked.
func setCodes(groups []tcgplayer.Group) map[int]string {
	ordered := append([]tcgplayer.Group(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].GroupID < ordered[j].GroupID
	})
	codes := map[int]string{}
	used := map[string]bool{}
	for _, group := range ordered {
		code := group.Abbreviation
		if used[code] {
			code = fmt.Sprintf("%s-%d", code, group.GroupID)
			log.Printf("%s: abbreviation %s already taken, set code %s minted",
				group.Name, group.Abbreviation, code)
		}
		used[code] = true
		codes[group.GroupID] = code
	}
	return codes
}

// mintedID is the card id given to a printing upstream does not carry: the
// negated product id. LorcanaJSON's ids are positive counting numbers, so
// the negative half of the space is unmistakably ours and cannot collide
// with an id upstream publishes later, however far its own numbering runs -
// and the product the card was minted from reads straight off it.
func mintedID(productID int) int {
	return -productID
}

// mintedNumber splits a catalog collector number into the integer upstream
// files a card under and the letter tail it calls the card's variant
// ("25a"), reading through the "/total" tail the catalog writes. A product
// with no number at all is filed under 0, as the numberless promos are.
func mintedNumber(code string) (int, string) {
	digits := number(code)
	i := 0
	for i < len(digits) && digits[i] >= '0' && digits[i] <= '9' {
		i++
	}
	num, _ := strconv.Atoi(digits[:i])
	return num, digits[i:]
}

// A promo type says what promoted a printing, or what treatment it wears
// beyond what its rarity and its set already say. Three things in this
// category say one, and each is read where it is written:
//
//   - promoSourceCategory, upstream's own word for where a promo came
//     from, on 252 cards;
//   - varnishType, the finish printed over the card, on 302;
//   - the parenthetical a minted product's name carries, which is all
//     there is to read on a printing upstream does not publish at all.
//
// promoGrouping is not one of them. It reads like a label - "P3", "CC1" -
// but the identifier says what it is: a promo is numbered "1/P3" the way a
// normal card is numbered "1/204", and the grouping is the denominator.
// Where a card is numbered is not what promoted it.

// nameQualRe finds the parentheticals a minted product's name carries. No
// upstream name has one, so this reads the minted printings alone.
var nameQualRe = regexp.MustCompile(`\(([^)]*)\)`)

// Which piece of a puzzle a card is, which of a numbered run, and how many
// the run holds. Every one of these is the printing's identity rather than
// a promotion: the puzzle inserts are sold as nine cards that make one
// picture, and "Top Left" is which card, not what promoted it.
var (
	piecePlaceRe = regexp.MustCompile(`^(?:Top|Middle|Bottom) (?:Left|Right|Middle|Center)$`)
	pieceCountRe = regexp.MustCompile(`^(?:Version )?\d+ of \d+$`)
	pieceSetRe   = regexp.MustCompile(`^Set of \d+$`)
)

// qualTrims are the words a parenthetical ends in that say nothing the rest
// of it does not: the errata version is the errata, and a promo says it is
// one by being filed as one.
var qualTrims = []string{" Promo", " Version"}

// universalVarnishes are the (set, rarity, varnish) triples where every card
// of that rarity in that set wears that varnish. Upstream records the finish
// per card, but it is chosen per rarity: the high gloss is on all 12 of set
// 9's Legendaries and all 18 of its Epics, and the snow hot foil on all 18
// of set 11's Enchanted. A label every card of a rarity carries tells two of
// them apart no better than the rarity does, and only the two that are not
// universal - one Special of set 4, one of set 8 - say anything.
func universalVarnishes(items []any) map[string]bool {
	held := map[string]int{}
	total := map[string]int{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		set, _ := item["setCode"].(string)
		rarity, _ := item["rarity"].(string)
		total[set+"|"+rarity]++
		if varnish, _ := item["varnishType"].(string); varnish != "" {
			held[set+"|"+rarity+"|"+varnish]++
		}
	}
	out := map[string]bool{}
	for key, n := range held {
		if n == total[key[:strings.LastIndex(key, "|")]] {
			out[key] = true
		}
	}
	return out
}

// keptQual is the label a parenthetical holds, empty where it holds none.
func keptQual(qual string) string {
	qual = strings.TrimSpace(qual)
	if qual == "" || piecePlaceRe.MatchString(qual) ||
		pieceCountRe.MatchString(qual) || pieceSetRe.MatchString(qual) {
		return ""
	}
	// foilTypes names the finish already, on this very card - whichever of
	// the catalog's foils the qualifier spells.
	if canonicalFinish(qual) == finishFoil {
		return ""
	}
	for _, trim := range qualTrims {
		if len(qual) > len(trim) && strings.EqualFold(qual[len(qual)-len(trim):], trim) {
			qual = qual[:len(qual)-len(trim)]
		}
	}
	return strings.TrimSpace(qual)
}

// promoTypesOf reads a card's labels off the three fields that carry one,
// lowercased the way every other datastore here spells a promo type, and
// with no label written twice.
func promoTypesOf(item map[string]any, universal map[string]bool) []string {
	var tags []string
	set, _ := item["setCode"].(string)
	rarity, _ := item["rarity"].(string)
	if varnish, _ := item["varnishType"].(string); varnish != "" &&
		!universal[set+"|"+rarity+"|"+varnish] {
		tags = append(tags, varnish)
	}
	// "Promo" is every promo's category and no promo's promotion: the
	// rarity and the set it is filed under say that much already.
	if source, _ := item["promoSourceCategory"].(string); source != "" &&
		!strings.EqualFold(source, "Promo") {
		tags = append(tags, source)
	}
	name, _ := item["fullName"].(string)
	for _, m := range nameQualRe.FindAllStringSubmatch(name, -1) {
		if qual := keptQual(m[1]); qual != "" {
			tags = append(tags, qual)
		}
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = promoSlug(tag)
		if tag == "" || slices.Contains(out, tag) {
			continue
		}
		out = append(out, tag)
	}
	return out
}

// finishesSold is the finish each of a card's printings is sold under, in
// the vocabulary TCGplayer prices them in: what the catalog says where it
// said anything, and the rule below where it did not.
//
// The rule is only for the printings TCGplayer sells none of - the minted
// promos and the handful upstream carries alone. Silver is the standard
// foil and everything else is the treatment past it; a card with two
// treatments and no standard has its first stand in for one, which is what
// TCGplayer does with the three that exist.
func finishesSold(item map[string]any, finishNames map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	if links, ok := item["externalLinks"].(map[string]any); ok {
		for _, name := range stringsOf(links["tcgPrintings"]) {
			// Kept in TCGplayer's own spelling, deduplicated by the finish
			// it names: two printings the matcher folds together are one
			// printing, and listing both would give the card two uuids
			// for one sku.
			finish := canonicalFinish(name)
			if finish == "" || seen[finish] {
				continue
			}
			seen[finish] = true
			out = append(out, name)
		}
	}
	if len(out) > 0 {
		return out
	}
	// The printings TCGplayer sells none of are named in its vocabulary
	// too, so one spelling reaches a finish whether the catalog placed it
	// or the rule below did - and the names come from the same dump either
	// way rather than from a list written here.
	var holo int
	for _, foilType := range stringsOf(item["foilTypes"]) {
		want := finishNonfoil
		switch {
		case canonicalFinish(foilType) == finishNonfoil:
		case isStandardFoil(foilType):
			want = finishFoil
		default:
			want = finishHolofoil
			holo++
		}
		// A finish the category does not sell names nothing: this rule is
		// for the printings the catalog lists no sku for, not a licence to
		// invent a printing it has no name for.
		name := finishNames[want]
		if name == "" || slices.Contains(out, name) {
			continue
		}
		out = append(out, name)
	}
	if standard := finishNames[finishFoil]; holo > 1 && standard != "" && !slices.Contains(out, standard) {
		for i, name := range out {
			if name == finishNames[finishHolofoil] {
				out[i] = standard
				break
			}
		}
	}
	return out
}

// catalogFinishes is the category's finish vocabulary, read from the dump:
// the name TCGplayer prices each finish under, keyed by the finish that name
// places to. Today that is Normal, Cold Foil and Holofoil, and this build is
// not the place that decides so - the category names its own printings, the
// dump carries them, and cmd/riftbound reads the same list the same way. A
// name neither vocabulary places is skipped rather than filed under itself:
// this map answers "what does the catalog call the plain one", and a
// treatment is not an answer to that.
func catalogFinishes(c *tcgplayer.CatalogDump) map[string]string {
	out := map[string]string{}
	for _, printing := range c.Printings {
		switch finish := canonicalFinish(printing.Name); finish {
		case finishNonfoil, finishFoil, finishHolofoil:
			// First seen wins, so a category that grows a second name for
			// one finish keeps answering with the one already published.
			if _, found := out[finish]; !found {
				out[finish] = printing.Name
			}
		}
	}
	return out
}

// finishOf is the finish a foil type is sold under: the standard foil where
// it is the standard one, and the treatment slot otherwise - unless the card
// has no treatment slot, in which case the treatment is what the standard
// one holds. Read against the finishes the card is actually sold in rather
// than assumed, so a card TCGplayer sells one way is not given two.
//
// The card's whole list of foil types decides, not the one name: three cards
// are foiled two ways and in neither of the standard ones (Simba, Megara and
// Robin Hood, all Whispers in the Well), so both names would claim the
// treatment slot and merge onto one uuid while the standard foil TCGplayer
// does sell went unreachable by any name upstream gives it. The first of
// them stands in for the standard foil instead, which is the same rule
// finishesSold applies where the catalog names nothing.
func finishOf(foilType string, foilTypes, sold []string) string {
	want := finishHolofoil
	switch {
	case canonicalFinish(foilType) == finishNonfoil:
		want = finishNonfoil
	case isStandardFoil(foilType) || standsInForStandard(foilType, foilTypes):
		want = finishFoil
	}
	// Answered with a name the card is sold under, spelled the way the
	// datastore spells it, rather than with one of ours.
	for _, name := range sold {
		if canonicalFinish(name) == want {
			return name
		}
	}
	if want == finishNonfoil {
		return ""
	}
	// One finish and two names for it: whichever foil the card is sold in
	// answers, because there is nothing else for the name to reach.
	for _, name := range sold {
		if canonicalFinish(name) != finishNonfoil {
			return name
		}
	}
	return ""
}

// standsInForStandard reports whether a treatment is the one holding the
// standard foil's place: the card names no standard foil of its own, names
// more than one treatment, and this is the first of them. Upstream lists a
// card's foil types in the order the catalog prices them, so the first is
// the one TCGplayer sells as the plain foil - Whispers in the Well's own
// "FreeForm1" beside the "RainbowPillars" past it.
func standsInForStandard(foilType string, foilTypes []string) bool {
	var treatments []string
	for _, name := range foilTypes {
		if canonicalFinish(name) == finishNonfoil {
			continue
		}
		if isStandardFoil(name) {
			// The card has a standard foil of its own to hold the slot.
			return false
		}
		treatments = append(treatments, name)
	}
	return len(treatments) > 1 && treatments[0] == foilType
}

// isStandardFoil reports whether a foil type is the cold foil almost every
// Lorcana card is foiled in, which TCGplayer sells as "Cold Foil".
func isStandardFoil(foilType string) bool {
	switch canonicalFinish(foilType) {
	case finishFoil, upstreamStandardFoil:
		return true
	}
	return false
}

// treatmentLabel is the promo type a foil type is worth carrying: the
// treatment past the plain foil, spelled the way a label is spelled here.
// The plain printing and the standard foil are not treatments, and neither
// is the one upstream calls "Holofoil" - that is the name the finish
// already wears, and a label saying what the finish says is no label.
func treatmentLabel(foilType string) string {
	if canonicalFinish(foilType) == finishNonfoil || isStandardFoil(foilType) ||
		canonicalFinish(foilType) == finishHolofoil {
		return ""
	}
	// No word seam is looked for: a slug drops the spaces a split would put
	// in, so "VerticalWave" reaches "verticalwave" either way.
	return promoSlug(runNumberRe.ReplaceAllString(foilType, ""))
}

// promoSlugRe is everything a promo type is spelled without.
var promoSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// promoSlug spells a label the way every promo type here is spelled: lower
// case, letters and digits and nothing else. It is a token for a consumer to
// interpret and a query to carry, not words for a reader - what a promotion
// is shown as is the loader's to decide, and a data file that spells one for
// display has decided it for every consumer at once.
func promoSlug(label string) string {
	return promoSlugRe.ReplaceAllString(strings.ToLower(label), "")
}

// runNumberRe finds the run number a treatment's name ends in. The same
// pattern printed again is numbered rather than renamed - "FreeForm1" and
// "FreeForm2" are one treatment over two runs, one in set 10 and one across
// the D23 promos - and which run a card came from is not what its treatment
// is. The number comes off, so both file under "free form" and a third run
// joins them without anything here being told about it.
var runNumberRe = regexp.MustCompile(`[0-9]+$`)

// cardID reads a card's id back off a decoded document: a number comes back
// as a float64, and a minted card's is negative.
func cardID(value any) (int, bool) {
	switch id := value.(type) {
	case int:
		return id, true
	case float64:
		return int(id), true
	}
	return 0, false
}

// stringsOf reads a list of strings back off a decoded document, where a
// slice this build wrote itself comes back as []any.
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

// printingUUID is the uuid a printing prices: the card's for the plain
// printing, and the card's with the finish on the end for a foil. The finish
// is the matcher's spelling of it rather than TCGplayer's - a uuid that
// moves resolves to nothing rather than erroring, so the name the datastore
// publishes for a finish and the name a uuid carries are kept apart. It has
// to agree with the loader exactly - a uuid spelled differently is a
// printing nothing resolves - so the two helpers below duplicate
// go-mtgban's, the way this repository duplicates every helper it shares
// rather than depending on it.
func printingUUID(id int, finish string) string {
	base := cardUUID(id)
	if finish != finishNonfoil {
		return base + "_" + finish
	}
	return base
}

// The finishes as the matcher spells them, which is what a uuid carries.
// There is no coldFoil here on purpose: every Lorcana foil is a cold foil,
// so the name TCGplayer prices the standard one under is the shared foil
// slot rather than a finish of its own - which is what lets a bare foil
// flag, all most storefronts send, reach it. mtgmatcher/lorcana folds
// "coldfoil" onto the same slot at the other end.
const (
	finishNonfoil  = "nonfoil"
	finishFoil     = "foil"
	finishHolofoil = "holofoil"
)

// upstreamStandardFoil is LorcanaJSON's name for that same printing. Three
// vocabularies name it and only this one is a foil type: "Silver" upstream,
// "Cold Foil" in the catalog, the plain foil slot in the matcher.
const upstreamStandardFoil = "silver"

// cardUUID is a card's uuid: its upstream id, and a minted card's negative
// id written as the "m-" the loader reads it back from.
func cardUUID(id int) string {
	if id < 0 {
		return fmt.Sprintf("m-%d", -id)
	}
	return strconv.Itoa(id)
}

// canonicalFinish folds a foil type name to the spelling the matcher keys a
// uuid by: no case and no separators, upstream's "None" placeholder as the
// plain printing, and the cold foil almost every card is foiled in as the
// standard foil.
func canonicalFinish(name string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			normalized.WriteRune(r)
		}
	}
	// The spellings below are the normalized forms of the names above -
	// "none" is LorcanaJSON's placeholder for a plain printing, "normal"
	// and "coldfoil" are the catalog's own names folded. A foil type
	// neither vocabulary places is handed back as itself, because the
	// vocabulary is data.
	switch folded := normalized.String(); folded {
	case "none", "normal":
		return finishNonfoil
	case "coldfoil", "foil":
		return finishFoil
	default:
		return folded
	}
}

// productLanguage names the language a product is printed in, empty for the
// English program: a product TCGplayer prices in no English sku is sold in
// another language, and the catalog's own language list spells out which.
// Several non-English languages on one product would be a shape this has
// never seen, so it is said out loud and the lowest id wins.
func productLanguage(names map[int]string, product tcgplayer.Product) string {
	var ids []int
	for _, sku := range product.Skus {
		if sku.LanguageID == englishLanguage {
			return ""
		}
		if !slices.Contains(ids, sku.LanguageID) {
			ids = append(ids, sku.LanguageID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Ints(ids)
	if len(ids) > 1 {
		log.Printf("%q (%d) prices skus in %d languages, filed under the first",
			product.Name, product.ProductID, len(ids))
	}
	return names[ids[0]]
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
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 71 (required)")
	source := flag.String("lorcana", "", "LorcanaJSON allCards file, path or URL (required)")
	against := flag.String("against", "", "baseline datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.01, "the share of its cards or sealed products a build may lose")
	baselineFit := flag.String("baseline-fit", "", "write this file when the build is fit to become the baseline the next build compares against")
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
	var catalog tcgplayer.CatalogDump
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != lorcanaCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, lorcanaCategory)
	}
	productByID := map[int]tcgplayer.Product{}
	// The coverage contract: every product the catalog types as a card.
	// validate reads it back off the encoded output, so a product no rule
	// here carried fails the build instead of leaving the datastore.
	cardProducts := map[int]bool{}
	for _, product := range catalog.Products {
		productByID[product.ProductID] = product
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
	printings := printingNames(&catalog)
	finishNames := catalogFinishes(&catalog)
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
		key := normalizeName(product.Name) + "|" + number(product.Extended("Number"))
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
	unclaimed := map[string][]tcgplayer.Product{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) || claimed[product.ProductID] {
			continue
		}
		num := product.Extended("Number")
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
	// union over its claimed and extra products. This is what says which
	// finishes a card has: a printing the catalog prices a sku for is one
	// that exists, and it is read straight rather than reconciled against
	// upstream's foil types, which describe how a foil looks rather than
	// what is sold.
	var printed int
	for _, c := range cards {
		if c.tcgID == 0 {
			continue
		}
		names := append([]string(nil), printings[c.tcgID]...)
		if extraIDs, ok := c.links["tcgPlayerExtraIds"].([]int); ok {
			for _, id := range extraIDs {
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
		printed++
	}
	log.Printf("printings: %d cards told which finishes the catalog sells them in", printed)

	// Mint a card for every single the catalog carries that no card
	// claimed or matched: the printings upstream has not published, the
	// unnumbered inserts and oversized components it has no concept for,
	// and the listings a card's own product join refused. A product
	// TCGplayer sells is a printing that exists, and a datastore leaving
	// it out leaves every listing of it unresolvable.
	groupByID := map[int]tcgplayer.Group{}
	for _, group := range catalog.Groups {
		groupByID[group.GroupID] = group
	}
	languageNames := map[int]string{}
	for _, language := range catalog.Languages {
		languageNames[language.LanguageID] = language.Name
	}
	var mintable []tcgplayer.Product
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		if claimed[product.ProductID] || matched[product.ProductID] {
			continue
		}
		mintable = append(mintable, product)
	}
	sort.Slice(mintable, func(i, j int) bool {
		return mintable[i].ProductID < mintable[j].ProductID
	})
	codes := setCodes(catalog.Groups)
	mintedByGroup := map[string]int{}
	for _, product := range mintable {
		group := groupByID[product.GroupID]
		num, variant := mintedNumber(product.Extended("Number"))
		links := map[string]any{"tcgPlayerId": product.ProductID}
		if names := printings[product.ProductID]; len(names) > 0 {
			links["tcgPrintings"] = names
		}
		item := map[string]any{
			"id":       mintedID(product.ProductID),
			"fullName": product.Name,
			"name":     product.Name,
			"setCode":  codes[group.GroupID],
			"number":   num,
			"rarity":   product.Extended("Rarity"),
			"images": map[string]any{
				"full":      imageURL(product.ImageURL),
				"thumbnail": product.ImageURL,
			},
			"externalLinks": links,
		}
		if variant != "" {
			item["variant"] = variant
		}
		if cardType := product.Extended("CardType"); cardType != "" {
			item["type"] = cardType
		}
		// A printing TCGplayer prices in no English sku is sold in another
		// language; the catalog's own language list says which. The
		// matcher drops a non-English candidate from a query that named no
		// language, so the row exists without English matching changing.
		if language := productLanguage(languageNames, product); language != "" {
			item["language"] = language
		}
		items = append(items, item)
		mintedByGroup[codes[group.GroupID]]++
	}
	log.Printf("minted: %d cards for products upstream does not carry, by group %v",
		len(mintable), mintedByGroup)

	// The labels, over both kinds of card at once: what upstream publishes
	// and what only a product name says are the same kind of fact, and a
	// rule written where the two meet cannot reach one and miss the other.
	universal := universalVarnishes(items)
	vocabulary := map[string]int{}
	var labelled, imaged int
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// The card's picture, under the name the other seven datastores
		// give it. Upstream publishes an images object with a full, a
		// thumbnail and the masks; every other game here publishes one
		// string called "image", and a consumer should not have to know
		// which game it is holding to find the picture.
		if _, found := item["image"]; !found {
			if images, ok := item["images"].(map[string]any); ok {
				if full, ok := images["full"].(string); ok && full != "" {
					item["image"] = full
					imaged++
				}
			}
		}
		types := promoTypesOf(item, universal)
		if len(types) == 0 {
			continue
		}
		item["promoTypes"] = types
		labelled++
		for _, t := range types {
			vocabulary[t]++
		}
	}
	log.Printf("image: %d cards given the common field beside upstream's images object", imaged)

	// The finish each printing is sold under, in TCGplayer's own words,
	// and the uuid each of those prices.
	//
	// Upstream names sixteen foil types where TCGplayer names three -
	// Normal, Cold Foil and Holofoil - and prices arrive in TCGplayer's
	// vocabulary, so that is the one a finish is keyed by. Derived instead,
	// the best rule available ("Silver is the standard foil, everything
	// else is the treatment past it") disagrees with TCGplayer on 28 of
	// 3,452 cards: seven SeaWave printings it sells as Cold Foil, and
	// fifteen foiled in plain silver it sells as Holofoil. Read rather than
	// derived, they agree by construction.
	//
	// What upstream calls the foil becomes a promo type, which is where the
	// other games already keep a treatment - One Piece's pirate foil, jolly
	// roger foil and textured foil are promo types beside a two-value
	// finish, and these are the same kind of fact.
	var named, withIDs, treatments, aliased int
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, ok := cardID(item["id"])
		if !ok {
			continue
		}
		sold := finishesSold(item, finishNames)
		if len(sold) == 0 {
			continue
		}
		// Keyed by the finish as TCGplayer prices it, that being the name
		// the datastore uses for it everywhere else; the uuid keeps the
		// matcher's spelling, which is the one already in circulation.
		ids := make(map[string]any, len(sold))
		for _, finish := range sold {
			ids[finish] = printingUUID(id, canonicalFinish(finish))
		}
		item["printingIds"] = ids
		named += len(ids)
		withIDs++

		// The spelling upstream gives a foil, and the finish it is sold
		// under. A storefront naming the treatment - "Rainbow Pillars" -
		// is naming a printing, and without this it would land on the
		// standard foil instead of the one it asked for.
		aliases := map[string]any{}
		for _, foilType := range stringsOf(item["foilTypes"]) {
			finish := finishOf(foilType, stringsOf(item["foilTypes"]), sold)
			name := canonicalFinish(foilType)
			// A spelling that already is the finish's own name reaches it
			// without an alias, whichever vocabulary each is written in.
			if finish == "" || name == "" || canonicalFinish(finish) == name {
				continue
			}
			aliases[name] = finish
		}
		if len(aliases) > 0 {
			item["finishAliases"] = aliases
			aliased += len(aliases)
		}

		// The treatment past the plain foil, named the way upstream names
		// it. The plain one is not a treatment and carries no label.
		for _, foilType := range stringsOf(item["foilTypes"]) {
			label := treatmentLabel(foilType)
			if label == "" {
				continue
			}
			types := stringsOf(item["promoTypes"])
			if slices.Contains(types, label) {
				continue
			}
			item["promoTypes"] = append(types, label)
			treatments++
		}
	}
	log.Printf("printing ids: %d uuids named over %d cards, so the loader spells none", named, withIDs)
	log.Printf("finishes: named in TCGplayer's words, %d foil treatments moved to a promo type, %d spellings kept reaching their printing", treatments, aliased)
	log.Printf("promo types: %d labels over %d cards, and %d varnishes left off as their rarity's own",
		len(vocabulary), labelled, len(universal))

	doc["cards"] = items

	// Sealed products: everything the catalog files outside the singles
	// type, from every group, in a top-level array a stock LorcanaJSON
	// reader ignores. Groups LorcanaJSON has no set for (the promotional
	// ones) get a set entry minted so every product's set exists, card
	// and sealed alike.
	groups := append([]tcgplayer.Group(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Abbreviation < groups[j].Abbreviation
	})
	productsByGroup := map[int][]tcgplayer.Product{}
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
	// The base run's size, under the name Pokemon and mtgjson give it.
	// Upstream calls it cardCounts.base and Riftbound's gallery calls it
	// collectorNumberMax; the fact is the same one, and a reader of this
	// file should not have to learn three names for it.
	var sized int
	for _, raw := range sets {
		set, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, found := set["baseSetSize"]; found {
			continue
		}
		counts, ok := set["cardCounts"].(map[string]any)
		if !ok {
			continue
		}
		if base, ok := counts["base"].(float64); ok && base > 0 {
			set["baseSetSize"] = int(base)
			sized++
		}
	}
	log.Printf("sets: %d given a baseSetSize beside upstream's cardCounts", sized)
	var sealedItems []any
	for _, group := range groups {
		var count int
		for _, product := range productsByGroup[group.GroupID] {
			if slices.Contains(tcgSingles, product.ProductType) {
				continue
			}
			sealedItems = append(sealedItems, map[string]any{
				"id":          fmt.Sprintf("%s-%d", strings.ToLower(codes[group.GroupID]), product.ProductID),
				"name":        product.Name,
				"setCode":     codes[group.GroupID],
				"releaseDate": group.ReleaseDate(),
				"image":       imageURL(product.ImageURL),
				"externalLinks": map[string]any{
					"tcgPlayerId": product.ProductID,
				},
			})
			count++
		}
		code := codes[group.GroupID]
		count += mintedByGroup[code]
		if count == 0 {
			continue
		}
		if _, found := sets[code]; !found {
			sets[code] = map[string]any{
				"name":        group.Name,
				"releaseDate": group.ReleaseDate(),
				"type":        "promo",
			}
			log.Printf("%s (%s): set minted for %d products", group.Name, code, count)
		}
	}
	if len(sealedItems) > 0 {
		doc["sealed"] = sealedItems
	}
	log.Printf("sealed: %d products", len(sealedItems))

	var buf bytes.Buffer
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	plainQuotes(doc)

	if err := json.NewEncoder(&buf).Encode(doc); err != nil {
		log.Fatalln(err)
	}

	// Re-read the encoded output and verify it structurally before
	// publishing anything: an upstream format change or a truncated
	// download must fail here, not in every consumer. The types mirror
	// what go-mtgban's mtgmatcher/lorcana reads, duplicated so this
	// repository depends on nothing.
	counted, err := validate(buf.Bytes(), cardProducts)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d tcgplayer ids, %d sealed",
		counted.sets, counted.cards, counted.identified, counted.sealed)
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped (%d minted, %d upstream)",
		counted.carried, len(cardProducts), len(cardProducts)-counted.carried,
		len(mintable), counted.carried-len(mintable))
	emitted := len(cards) + len(mintable)
	if counted.cards != emitted || counted.sealed != len(sealedItems) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			emitted, len(sealedItems), counted.cards, counted.sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a card, so it is exhaustive by construction
	// and cannot lose a product to a rule that did not know what to do with
	// it - the card side's whole failure mode. What it can lose a product
	// to is an edit: one `continue` on the sealed path and the products
	// would leave the datastore with nothing to say so, the card side's
	// invariant being blind to them. Counting the emitted products back
	// against the catalog total is what says so.
	wantSealed := len(catalog.Products) - singles
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
				log.Print("baseline: unchanged, this build holds less than it does")
			} else {
				note := fmt.Sprintf("cards=%d sealed=%d\n", current.cards, current.sealed)
				if err := os.WriteFile(*baselineFit, []byte(note), 0o644); err != nil {
					log.Fatalln("baseline:", err)
				}
				log.Print("baseline: this build becomes the one the next is measured against")
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
	sets, cards, sealed, identified, carried int
}

// validate decodes an encoded datastore and checks its shape: sets and
// cards present, every card and sealed product carrying its identity,
// every id unique within its namespace, every sealed set existing, and
// every product the catalog types as a card claimed by a card — the
// zero-skip invariant, checked on the encoded output so a product no rule
// above carried stops the publish instead of leaving the datastore.
// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func validate(data []byte, cardProducts map[int]bool) (counts, error) {
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
				TcgPlayerID     int   `json:"tcgPlayerId"`
				TcgPlayerExtras []int `json:"tcgPlayerExtraIds"`
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

	// The date is not required: upstream lists future sets before their
	// release date is announced.
	for code, set := range doc.Sets {
		if set.Name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
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
		for _, id := range append([]int{card.ExternalLinks.TcgPlayerID}, card.ExternalLinks.TcgPlayerExtras...) {
			if id == 0 {
				continue
			}
			if previous, found := claimedBy[id]; found {
				return out, fmt.Errorf("tcgplayer product %d claimed by both %s and %s", id, previous, claimant)
			}
			claimedBy[id] = claimant
		}
		if card.ExternalLinks.TcgPlayerID != 0 {
			out.identified++
		}
	}
	var missing, foreign []int
	for id := range claimedBy {
		if !cardProducts[id] {
			foreign = append(foreign, id)
			continue
		}
		out.carried++
	}
	for id := range cardProducts {
		if _, found := claimedBy[id]; !found {
			missing = append(missing, id)
		}
	}
	sort.Ints(missing)
	sort.Ints(foreign)
	if len(missing) > 0 {
		return out, fmt.Errorf("%d catalog card products carry no card, first is %d",
			len(missing), missing[0])
	}
	// A claim naming no card product is upstream's to make, not this
	// build's: the dump can lag a day behind a product upstream already
	// links, so it is said out loud rather than refused.
	if len(foreign) > 0 {
		log.Printf("%d claimed product ids the catalog types as no card, first is %d",
			len(foreign), foreign[0])
	}
	sealedIDs := map[string]bool{}
	for _, product := range doc.Sealed {
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerID == 0 {
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
