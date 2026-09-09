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
	// Source is the product the printing was handed out in - the set for
	// most cards, a promotional pack or accessory set for the rest. It is
	// what justifies every hand-carried row below, and is checked against
	// them so a row whose upstream reason disappears is reported.
	Source string `json:"where_to_get"`
}

// handCarried is a printing the game hands out that TCGplayer sells no
// single of, and that shares its collector number with a printing the
// catalog does sell - so the mint keyed on an uncarried number never
// reaches it. Each row names the promotional product it came in, and that
// name is both the variant label the entry carries and the string gcg-api
// has to agree with for the row to stand.
//
// Only the printings a storefront actually offers are here. A general mint
// off gcg-api's own field would carry 480 entries of very mixed quality:
// upstream files a card's own set under it too, spells the promotional
// products differently from the catalog, and writes "Events" and "-" where
// it knows nothing. Anchoring instead to the labels the catalog already
// uses carries one. Between the two there is no rule to write, so these are
// named one at a time and justified one at a time.
type handCarried struct {
	// number is the card's collector number, and names the printing this
	// entry reads its name, type, color and rarity from.
	number string
	// label is the promotional product the printing was handed out in,
	// spelled as the catalog spells the ones it does sell. It becomes the
	// entry's variant, which is what tells this printing from the ordinary
	// card sharing its number.
	label string
	// source is gcg-api's own spelling of the same product. The two differ
	// - upstream writes "Premium Card Collection 02" where the catalog
	// writes "Premium Card Collection" - so the row carries both and the
	// build checks that upstream still says it.
	source string
}

// handCarriedPrintings are the promotional reprints a storefront sells and
// TCGplayer does not, each one a card of an ordinary set handed out again
// in a promotional product. Without them a listing of one resolves to the
// ordinary card at the same number and is published at its identity.
var handCarriedPrintings = []handCarried{
	{number: "GD01-073", label: "Premium Card Collection 02", source: "Premium Card Collection 02"},
	{number: "GD03-101", label: "Premium Card Collection 02", source: "Premium Card Collection 02"},
	{number: "GD04-063", label: "Premium Card Collection 02", source: "Premium Card Collection 02"},
	{number: "GD05-110", label: "Premium Card Collection 02", source: "Premium Card Collection 02"},
	{number: "GD05-114", label: "Premium Card Collection 02", source: "Premium Card Collection 02"},
	{number: "ST03-006", label: "Premium Card Collection 02", source: "Premium Card Collection 02"},
	{number: "ST04-012", label: "1st Anniversary Event Pack", source: "1st Anniversary Event Pack"},
	// Not an event pack, whatever a storefront files it beside. gcg-api
	// knows the phrase "1st Anniversary Event Pack" and spends it on two
	// cards, ST04-012 above and EXBP-028; for this one it writes a prize
	// instead. A source that had the word and chose another is naming a
	// second printing, not the same one twice.
	{number: "GD01-100", label: "Serial Numbered Card Challenge Upper Ranks Prize", source: "Serial Numbered Card Challenge Upper Ranks Prize"},
}

// promoSetCode is the set the catalog files a promotional reprint under,
// and so where a hand-carried one belongs: the printings TCGplayer does
// sell of this kind are all filed there.
const promoSetCode = "GCG-PR"

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

// numberFor is the collector number a product carries, as the catalog
// files it.
func numberFor(p tcgplayer.Product) string {
	number := p.Extended("Number")
	if strings.EqualFold(number, "N/A") {
		number = ""
	}
	return number
}

// renumberCollisions repairs the one way this catalog contradicts itself
// about a collector number: two products of one group filed under the same
// number while their names spell different ones. "Resource (RP-045)" and
// "Resource (RP-046)" both sit at RP-045, in a run whose names read 045,
// 046, 047, 048 and 049, so the field on the second is the row above it
// repeated and the name is what the card says.
//
// Only a collision is repaired, never a lone disagreement, and that
// restraint is the whole of the rule. The catalog also files "EX Resource
// (EXR-003)" under EXR-002 with nothing else in its group at that number,
// and there the field is right: these cards are reprinted deck after deck,
// so a starter deck carrying EXR-002 again is ordinary, and the upstream
// naming only one set per printing cannot say otherwise. A rule that
// trusted the name outright got that second case wrong - it moved a priced
// product off the number it belongs to and left the number it had invented
// to be minted, unpriced and unillustrated.
func renumberCollisions(singles []single) {
	type slot struct {
		group  int
		number string
	}
	held := map[slot][]*single{}
	for i := range singles {
		if singles[i].number == "" {
			continue
		}
		held[slot{singles[i].product.GroupID, singles[i].number}] = append(
			held[slot{singles[i].product.GroupID, singles[i].number}], &singles[i])
	}
	// Map order is not an order, and this logs what it repairs: read back
	// two runs of the same catalog and the lines would be shuffled between
	// them, so a build log could not be diffed against the one before it.
	// The repair itself does not care - each product's new number is read
	// off its own name - but the record of it should be stable, the way the
	// mint below sorts for the same reason.
	slots := make([]slot, 0, len(held))
	for key := range held {
		slots = append(slots, key)
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].group != slots[j].group {
			return slots[i].group < slots[j].group
		}
		return slots[i].number < slots[j].number
	})

	var repaired int
	for _, key := range slots {
		bucket := held[key]
		if len(bucket) < 2 {
			continue
		}
		sort.Slice(bucket, func(i, j int) bool {
			return bucket[i].product.ProductID < bucket[j].product.ProductID
		})
		for _, s := range bucket {
			spelled := spelledNumberRe.FindStringSubmatch(s.product.Name)
			if spelled == nil || spelled[1] == key.number {
				continue
			}
			log.Printf("number: %q (%d) shares %s with another product and names %s; taking the name",
				s.product.Name, s.product.ProductID, key.number, spelled[1])
			s.number = spelled[1]
			// The parenthetical now restates the number, which decompose
			// strips wherever the two already agreed.
			var kept []string
			for _, q := range s.quals {
				if !strings.EqualFold(q, s.number) {
					kept = append(kept, q)
				}
			}
			s.quals = kept
			repaired++
		}
	}
	if repaired > 0 {
		log.Printf("number: %d products renumbered off a collision", repaired)
	}
}

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

// subjects name what a card is or what is drawn on it rather than what
// promoted it: the mobile suit's form or part, the type it is built as, the
// faction whose emblem a Resource token carries, the series an EX Base is
// illustrated from. Every one of them is really a name parenthetical the
// election could not learn, because it learns from a collector number sold
// as several products and each of these is sold as one - the eleven
// Promotional Resource Tokens are eleven numbers with one product apiece,
// RP-011 through RP-021, one faction on each. Nothing promoted a Resource
// for carrying the Zeon emblem, so the faction stays the variant it is.
//
// Named rather than read off the data for the same reason cmd/onepiece
// names the DON!! subjects: nothing in the data tells a subject from a
// treatment, and the test that comes closest - a label on one printing
// alone - takes "SDCC 2026" and "Launch Kit 01" along with the factions.
// parts name which piece of a multi-part card or token a printing is. They
// read like the subjects below - both are what the card shows rather than
// what promoted it - but a part is the whole of what tells one printing from
// another: the Wire-Guided Arm token comes as a left hand and a right hand,
// at one number, one rarity and one event, and dropping the part leaves the
// two indistinguishable. A form or a faction never does that; it describes a
// card that is already told apart by its number.
//
// So a part stays a promo type. What identifies a printing is exactly what a
// promo type is for.
var parts = map[string]bool{
	"head": true, "left hand": true, "right hand": true, "tail booster": true,
	"gn full shield": true, "gn high mega launcher": true,
	"mirasoul flight unit": true, "apfsds round": true,
}

var subjects = map[string]bool{
	// The form a mobile suit is in, and the mode it transforms to.
	"2nd form": true, "middle form": true, "fighter mode": true,
	"trooper mode": true, "waverider mode": true, "tail unit flight mode": true,
	// The type or the custom a unit is built as.
	"armored rru type": true, "ground heavy equipment type": true,
	"heavy armed type": true, "guards type": true, "type-e": true,
	"graze custom ii": true, "ryusei-go": true, "meteor": true,
	// Who or what is drawn on it.
	"char aznable emblem": true, "four snake eyes'": true,
	"enhanced person number 5": true, "red": true,
	// The faction a Resource token carries.
	"earth alliance": true, "earth federation force": true,
	"league militaire": true, "militia": true, "neo zeon": true, "oz": true,
	"peacemaker team": true, "sanc kingdom": true,
	"united emirates of orb": true, "vist foundation": true,
	"zaft": true, "zeon force": true,
	"asticassia school of technology": true,
	// The series an illustration comes from.
	"mobile suit gundam: hathaway's flash":     true,
	"mobile suit gundam: iron-blooded orphans": true,
}

// bareNumberingRe matches a label that is a collector number and nothing
// else. The catalog writes a number in a parenthesis where the card is two
// cards - "Core Booster (005) & Core Booster (006)" - and where it names a
// number the Number field spells differently: "EX Resource (EXR-003)" is
// filed at EXR-002. A number is not a promotion either way.
var bareNumberingRe = regexp.MustCompile(`(?i)^(?:[a-z]{1,4}-)?\d{2,4}$`)

// spacedNumberRe puts back the space the catalog drops before a number, so
// one family does not read two ways: it writes the season both "WCS 26-27"
// and "WCS26-27", the volume both "Vol. 2" and "Vol.1", and the mission
// both "Mission 1" and "Mission1".
var spacedNumberRe = regexp.MustCompile(`(?i)\b(Vol\.|WCS|Mission)(\d)`)

// spelledQual writes a label the one way this datastore spells it.
//
// "Participant Pack" is TCGplayer's own slip and the sealed side is the
// evidence: the five products are called "Store Tournament Participation
// Pack 01" through 05, while the singles say "Participant" on the first
// four and "Participation" on the fifth. The pack has one name.
//
// "SP Ver." is the SP treatment with a word after it, and "SDCC" is the
// convention the other printing of it spells out.
var spelledQual = strings.NewReplacer(
	"Participant Pack", "Participation Pack",
	"SDCC", "San Diego Comic-Con",
	"SP Ver.", "SP",
)

// promoTypesOf is the labels a printing carries, one at a time, spelled the
// one way and lowercased the way every datastore here spells a promo type.
// It reads the qualifiers rather than the variant string they were joined
// into, because "Link Rare" is one label and splitting the string would
// make it two.
func promoTypesOf(quals []string) []string {
	out := make([]string, 0, len(quals))
	for _, qual := range quals {
		qual = spelledQual.Replace(qual)
		qual = spacedNumberRe.ReplaceAllString(qual, "$1 $2")
		tag := strings.ToLower(strings.Join(strings.Fields(qual), " "))
		if tag == "" || subjects[tag] || bareNumberingRe.MatchString(tag) {
			continue
		}
		// A part is carried whole: it is not an occasion to be folded to,
		// and the rules below would take "gn high mega launcher" for a
		// name too long rather than the one thing naming that printing.
		if parts[tag] {
			if !slices.Contains(out, tag) {
				out = append(out, tag)
			}
			continue
		}
		// Words, not slugs: subjects is keyed by them, and foldPromoTypes
		// below still has to read the seams between them. The slug is put
		// on there, once the whole vocabulary is in hand.
		for _, named := range namedPromotions(tag) {
			if named != "" && !slices.Contains(out, named) {
				out = append(out, named)
			}
		}
	}
	return out
}

// releaseTail is the kind of release a qualifier names, where it names one.
var releaseTail = regexp.MustCompile(`^.+\s+(\w+\s+release)$`)

// packSeam is the occasion a qualifier names and the pack handed out at it.
var packSeam = regexp.MustCompile(`^(.+?)\s+(\w+\s+pack)$`)

// namedPromotions is the promotions one qualifier names, in the words it
// names them in. A qualifier naming more than one becomes more than one:
// "Store Tournament Winner Pack 01" is a store tournament, and a winner
// pack, and knowing which of the five it was is not either of those.
func namedPromotions(tag string) []string {
	tag = generalPromotion(tag)
	// What is released is the set the card is in, which the card already
	// says, so only the kind of release is a promotion of it. It is the
	// same reason the set code comes off "GD05 Release Event" above.
	if m := releaseTail.FindStringSubmatch(tag); m != nil {
		return []string{m[1]}
	}
	if m := packSeam.FindStringSubmatch(tag); m != nil {
		return []string{m[1], m[2]}
	}
	return []string{tag}
}

// generalPromotion drops what a promotion is not. Which instalment of it this
// was, which year it ran, and which set it released are all facts about the
// printing that the number, the set and the variant already carry - and
// keeping them made a promo type of every run, so a query naming the
// promotion reached one of them and none of the rest.
func generalPromotion(tag string) string {
	for abbrev, spelled := range promoAbbrevs {
		tag = regexp.MustCompile(`\b`+abbrev+`\b`).ReplaceAllString(tag, spelled)
	}
	tag = setCodeHead.ReplaceAllString(tag, "")
	tag = setCodePair.ReplaceAllString(tag, "")
	tag = runNumbering.ReplaceAllString(tag, "")
	tag = runYear.ReplaceAllString(tag, "")
	return strings.Join(strings.Fields(tag), " ")
}

// promoAbbrevs are the two the catalog writes both ways, so one promotion is
// one token however a product name happens to spell it.
var promoAbbrevs = map[string]string{
	"sdcc": "san diego comic-con",
	"wcs":  "world championship",
}

var (
	// The set a release event released, which the card's own set says.
	setCodeHead = regexp.MustCompile(`^(?:gd|st|evx|eb)\d+[a-z]?\s+`)
	setCodePair = regexp.MustCompile(`\s*/\s*(?:gd|st|evx|eb)\d+[a-z]?\b`)
	// Which instalment: a run code, a volume, a mission, a season, a number.
	runNumbering = regexp.MustCompile(`\s*-\s*pc\d+[a-z]?$|\s+(?:vol\.?|no\.?)\s*\d+$|\s+(?:mission|season)\s+\d+$|\s+\d+$`)
	// Which year, written either way the catalog writes one.
	runYear = regexp.MustCompile(`\s+\d{2}-\d{2}\b|\s+(?:19|20)\d{2}\b`)
)

// promoSlugRe is everything a promo type is spelled without.
var promoSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// promoSlug spells a label the way every promo type here is spelled: lower
// case, letters and digits and nothing else.
func promoSlug(label string) string {
	return promoSlugRe.ReplaceAllString(strings.ToLower(label), "")
}

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
// never said it - "strings.Contains(strings.ToLower(set.Name), "promotional")" - which is the same fact
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
	renumberCollisions(singles)

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

	var cards []any
	for _, s := range singles {
		productID := s.product.ProductID
		for _, finish := range orderedFinishes(printings[productID], displayOrder) {
			entry := map[string]any{
				"id":      idBase(s.number, productID) + finishSuffix(finish),
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
				if tags := promoTypesOf(s.quals); len(tags) > 0 {
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
			"finish":  plainPrinting(&catalog),
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
	// The promotional reprints a storefront sells and TCGplayer does not.
	// They share a collector number with the ordinary card, so the mint
	// above never reaches them: it fires on a number nothing carries.
	//
	// Each row stands down the moment the catalog sells the printing. What
	// the build already names it leaves alone - the test is the identity
	// and not the number, since the whole point of these is to sit beside
	// an entry at the same number - and the real product wins, prices and
	// artwork and all.
	carriedIdentity := map[string]bool{}
	plainByNumber := map[string][]map[string]any{}
	for _, entry := range cards {
		e, isMap := entry.(map[string]any)
		if !isMap {
			continue
		}
		number, _ := e["number"].(string)
		variant, _ := e["variant"].(string)
		setCode, _ := e["setCode"].(string)
		carriedIdentity[number+"|"+setCode+"|"+variant] = true
		if number != "" {
			plainByNumber[number] = append(plainByNumber[number], e)
		}
	}
	upstreamSource := map[string]map[string]bool{}
	for _, u := range upstream {
		if u.Number == "" || u.Source == "" {
			continue
		}
		if upstreamSource[u.Number] == nil {
			upstreamSource[u.Number] = map[string]bool{}
		}
		upstreamSource[u.Number][u.Source] = true
	}

	var handCarriedCount, stoodDown int
	if _, known := sets[promoSetCode]; !known && len(handCarriedPrintings) > 0 {
		log.Fatalf("hand-carried: set %s holds no product here; refusing to file promotional reprints nowhere", promoSetCode)
	}
	for _, printing := range handCarriedPrintings {
		// Upstream is what says the printing exists. A row it no longer
		// agrees with is reported rather than carried: the alternative is
		// a printing this build asserts on nobody's authority.
		if !upstreamSource[printing.number][printing.source] {
			log.Printf("hand-carried: gcg-api no longer says %s came in %q; not carried",
				printing.number, printing.source)
			continue
		}
		if carriedIdentity[printing.number+"|"+promoSetCode+"|"+printing.label] {
			log.Printf("hand-carried: %s %q is sold as a product now; the hand-carried one stands down",
				printing.number, printing.label)
			stoodDown++
			continue
		}
		// The ordinary card at this number is what the entry reads its
		// name and printed details from; a promotional reprint is that
		// card handed out again, not a card of its own.
		var base map[string]any
		for _, e := range plainByNumber[printing.number] {
			if variant, _ := e["variant"].(string); variant == "" {
				base = e
				break
			}
		}
		if base == nil {
			log.Printf("hand-carried: %s names no plain printing to read from; not carried", printing.number)
			continue
		}
		id := idStem(printing.number + "-" + printing.label)
		if id == "" || carriedIdentity[printing.number+"|"+promoSetCode+"|"+printing.label] {
			log.Printf("hand-carried: %s %q mints no usable id; not carried", printing.number, printing.label)
			continue
		}
		entry := map[string]any{
			"id":      id,
			"name":    base["name"],
			"number":  printing.number,
			"setCode": promoSetCode,
			"rarity":  base["rarity"],
			"finish":  plainPrinting(&catalog),
			"variant": printing.label,
		}
		if tags := promoTypesOf([]string{printing.label}); len(tags) > 0 {
			entry["promoTypes"] = tags
		}
		for _, field := range []string{"type", "color"} {
			if v, found := base[field]; found {
				entry[field] = v
			}
		}
		carriedIdentity[printing.number+"|"+promoSetCode+"|"+printing.label] = true
		cards = append(cards, entry)
		handCarriedCount++
	}
	log.Printf("hand-carried: %d promotional reprints carried, %d stood down for a product the catalog now sells",
		handCarriedCount, stoodDown)

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
	spelled, tokens := foldPromoTypes(cards)
	log.Printf("promo types: %d spellings folded to %d tokens, each one a slug", spelled, tokens)
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

// orderedFinishes fixes the order a product's entries are emitted in: the
// order TCGplayer displays the category's printings in, which is the
// catalog's to decide. Two printings can share a displayOrder, so the name
// settles a tie and unchanged data keeps producing byte-identical output.
func orderedFinishes(names []string, rank map[string]int) []string {
	out := slices.Clone(names)
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank[out[i]], rank[out[j]]; ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
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

// finishSuffix is the id suffix a printing's entries carry: nothing for the
// plain printing, and the printing's own name for every other. TCGplayer
// calls a plain printing "Normal" in every category, which is the one
// convention here rather than a list of this category's printings - those
// are the catalog's to name, to add to and to rename, and every one of them
// reaches an id without a release.
func finishSuffix(name string) string {
	if slug := finishSlug(name); slug != "" && slug != "normal" {
		return "_" + slug
	}
	return ""
}

// finishSlug spells a printing name the way an id carries it.
func finishSlug(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// plainPrinting is the catalog's name for the printing a bare id belongs to,
// or "" where the category has none - Yu-Gi-Oh prices its cards by print run
// and sells no printing it calls plain.
func plainPrinting(c *tcgplayer.CatalogDump) string {
	for _, printing := range c.Printings {
		if finishSlug(printing.Name) == "normal" {
			return printing.Name
		}
	}
	return ""
}

// stringsOf reads a list of strings back off a decoded entry, which holds
// them as []any once they have been through the map the document is built in.
func stringsOf(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, raw := range list {
			if s, ok := raw.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// promoTypeLimit is how long a promo type may read before only its first two
// words are kept. A token is what a query carries and what a reader sees
// beside a price, and "premiumaccessorysetmobilesuitgundamwing" is neither -
// the words are all in the variant, where the whole of a name belongs.
//
// 22 is the longest token this game has that is already the right name
// ("starterdeckbattleevent"), so the rule reaches past what is right and no
// further.
const promoTypeLimit = 22

// foldPromoTypes spells every promo type as its slug, once the whole
// vocabulary is in hand, and folds two things a card cannot see on its own.
//
// A name that is a longer form of one the vocabulary already holds is that
// one, said more narrowly: "Premium Card Collection Gundam Assemble" is a
// premium card collection, and which collection it was is the variant's to
// say. And a name still reading long past that has only its first two words
// kept, which is where the concept is and the rest is the instance of it.
func foldPromoTypes(cards []any) (int, int) {
	named := map[string]bool{}
	for _, raw := range cards {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, tag := range stringsOf(item["promoTypes"]) {
			named[tag] = true
		}
	}
	spelled := len(named)

	folded := make(map[string]string, len(named))
	for tag := range named {
		folded[tag] = promoSlug(shorterName(tag, named))
	}

	tokens := map[string]bool{}
	for _, raw := range cards {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		was := stringsOf(item["promoTypes"])
		if len(was) == 0 {
			continue
		}
		out := make([]any, 0, len(was))
		for _, tag := range was {
			slug := folded[tag]
			if slug == "" || slices.Contains(out, any(slug)) {
				continue
			}
			out = append(out, slug)
			tokens[slug] = true
		}
		if len(out) == 0 {
			delete(item, "promoTypes")
			continue
		}
		item["promoTypes"] = out
	}
	return spelled, len(tokens)
}

// shorterName is the name a promotion is known by: the shortest head of it
// the vocabulary already holds, or its first two words where it still reads
// past promoTypeLimit.
func shorterName(tag string, named map[string]bool) string {
	words := strings.Fields(tag)
	for i := 2; i < len(words); i++ {
		if head := strings.Join(words[:i], " "); named[head] {
			return head
		}
	}
	if len(words) > 2 && len(promoSlug(tag)) > promoTypeLimit {
		return strings.Join(words[:2], " ")
	}
	return tag
}
