// Command onepiece builds the One Piece datastore file consumed by
// go-mtgban's mtgmatcher/onepiece loader, from the TCGplayer catalog dump
// for category 68 and the punk-records mirror of Bandai's official card
// list.
//
// The Bandai printing id is annotated where Bandai's own pack says which
// printing a product is. Every printing in the list carries the pack it
// was handed out in, and packs.json labels that pack with the set code the
// catalog files the product under, so a product and an id are the same
// printing only where the product's group and the id's pack are one set:
// the plain product of the number's own set is the bare id, a Reprint
// product is an _rN id and any other variant a _pN, and one product
// against one id of a kind in one pack is that id.
//
// Counting the two sources' printings and handing ids out by position,
// which is what this did before, named 1,936 products with an id from
// another pack - a pre-release stamp took the starter deck's parallel, a
// starter deck's reprint took the booster's - and the clean image each
// carried, keyed by that id, was of the wrong card.
//
// What pack agreement cannot settle is left unnamed rather than ordered.
// Two products against two ids of a kind in one pack - a set's Alternate
// Art beside its Manga art - are told apart by nothing either source
// writes down. And the promotional printings stay unnamed as they always
// were: the list files every promo of every card in one of two unlabelled
// packs, while the catalog names the event each was handed out at - Judge
// Pack Vol. 3, Online Regional 2023, eighty of them - and no field on
// either side joins the two. They, the DON!! cards the game never numbers,
// and the printings sold in no English sku carry no Bandai id, and this
// source cannot supply one.
//
// Identity is the catalog's, one entry per English product and sku
// printing: TCGplayer prices Normal and Foil as separate sku printings of
// one product, so each printing is its own entry with its own id, priced
// by construction — the single-entry-per-product shape this datastore used
// to publish folded both price points onto one id where a product carries
// both. The id's finish suffix derives from the printing name alone, never
// from which sibling printings exist, so an id cannot churn when TCGplayer
// later adds a printing to a product. The recon census measured only 82%
// of collector numbers aligning between the two sources by count alone —
// Bandai's _pN ordinals are annotation here, attached where the alignment
// is unambiguous, never identity.
//
// The name parentheticals TCGplayer decorates products with fall into three
// kinds, told apart per collector number: a parenthetical every product of
// the number carries is part of the card's name (the "(Bentham)" epithets);
// a number disambiguator ("(003)", "(OP01-003)") is dropped; whatever
// remains is the variant label ("Alternate Art", "Manga", "SP", event
// names) the matcher narrows on.
//
// Every product the catalog types as a card becomes an entry, and validate
// refuses a build that left one out: a shape nobody has seen yet stops the
// publish instead of vanishing from the datastore. The Japanese-version
// promos TCGplayer sells are products like any other, so they are carried
// with the language their name calls them out as — the category prices
// them through English skus, so the name is the only source there is — and
// the matcher drops a non-English candidate from a query that named no
// language, leaving English matching exactly as it was.
//
// A card the game gives no collector number is filed under its card type:
// the DON!! cards, which neither the catalog nor Bandai's own card list
// numbers, and the promo Leaders handed out in sealed-battle packs. A type
// is stable where a counted or ordered number would renumber the cards the
// day TCGplayer adds a product, and it reads as what the card is rather
// than as a number a storefront's stray digits could match; identity falls
// to the set and the variant label, which the catalog spells out per
// product and no two of them share.
//
// The official card list also holds printings whose collector number the
// catalog has no product for, and those would have to be minted for this
// datastore to be the sum of both sources rather than the catalog alone.
// Today there are none — every number Bandai publishes has a product, the
// catalog carrying half again as many printings as the list does — so
// nothing is minted here and the count is reported at build time instead.
// The day it stops being zero the log names it, and the set a minted card
// would be filed under can be worked out against real cases: the list says
// which set a card belongs to only through the prefix of its number, and
// only 16 of the 60 prefixes match a catalog abbreviation, so a mapping
// guessed now against no case at all would mint duplicate sets for sets
// the catalog already carries.
//
// Sets are the catalog groups. Abbreviations repeat across groups, so codes
// are claimed in group-id order: the first group to claim an abbreviation
// keeps it bare, a later one carries its own group id as a suffix, and a
// blank abbreviation is minted from the group id. A set code so decided
// depends on the groups that came before it and never on the ones that come
// after, so an existing set keeps its code the day TCGplayer files a new
// group under an abbreviation it already uses, and the build refuses to
// publish unless it emitted one set per group.
//
// Sealed products are everything the catalog files outside the singles
// type, same as the other games: by exclusion, so a product type TCGplayer
// adds later lands on the sealed side where it is noticed.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mtgban/datastore-gen/internal/baseline"
	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/go-tcgplayer"
)

const (
	onepieceCategory = 68

	// donCardType is the card type the catalog gives every DON!! card. The
	// rarity does not answer for them - TCGplayer files the event ones as
	// "PR" alongside the promo characters - but the type does.
	donCardType = "DON!!"

	// donNumber is the shorter spelling the DON!! cards' ids were published
	// under, kept so this datastore's oldest minted numbers do not churn;
	// every other unnumbered card is filed under its card type as spelled.
	donNumber = "DON"

	punkCardsURL = "https://raw.githubusercontent.com/buhbbl/punk-records/main/english/index/cards_by_id.json"
	punkPacksURL = "https://raw.githubusercontent.com/buhbbl/punk-records/main/english/packs.json"
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; everything else is sealed by exclusion.
var tcgSingles = tcgplayer.SinglesProductTypes(onepieceCategory)

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.
//
// The printing names come from the dump's own CatalogDump.PrintingNames,
// which orders them as the category lists its printings. Nothing downstream
// reads that order — the loader tells a product's finishes apart by the
// "_foil" suffix on the id, which derives from the printing name alone — so
// the order is the dump's to choose.

// punkCard is the slice of a punk-records printing this build reads: the
// _pN-suffixed card id is Bandai's own printing identity, mirrored from
// the official card list.
type punkCard struct {
	CardID string `json:"card_id"`
	Name   string `json:"name"`
	ImgURL string `json:"img_url"`

	// PackID is the Bandai product the printing was handed out in, which
	// is what pins a variant to a set without guessing at its ordinal.
	PackID string `json:"pack_id"`
}

// punkPack is the slice of a punk-records pack this build reads: the set
// code Bandai labels the product with, where it labels one at all. The two
// promo packs carry none, which is exactly why the promotional printings
// cannot be told apart from this source.
type punkPack struct {
	TitleParts struct {
		Label string `json:"label"`
	} `json:"title_parts"`
}

// cardImage picks the card's image. Every official image of the game -
// Bandai's own card list and TCGplayer's copy of it alike - wears a giant
// SAMPLE watermark, and the community onepiece.gg mirror keys its cleaned
// renditions by the same Bandai printing id the datastore aligns, so the
// clean image is derivable exactly where the printing identity is known:
// the aligned printings by their id, the base printings by their bare
// number. An unaligned variant keeps the watermarked catalog image, whose
// art is at least the right one, and so does every DON!! card: the number
// they are filed under is this builder's, not a printing id the mirror
// could know.
func cardImage(s single, bandaiID string) string {
	if bandaiID != "" {
		return "https://static.dotgg.gg/onepiece/card/" + bandaiID + ".webp"
	}
	if len(s.quals) == 0 && s.number != donNumber {
		return "https://static.dotgg.gg/onepiece/card/" + s.number + ".webp"
	}
	return emit.ImageURL(s.product.ImageURL)
}

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)

// bracketRe matches the bracketed tail this category writes a prize card's
// placement in.
var bracketRe = regexp.MustCompile(`\s*\[([^\]]+)\]`)
var bareNumRe = regexp.MustCompile(`^\d{3}$`)

// languageWords maps the word a product name calls a printing out with to
// the language the entry carries. The category prices every product through
// English skus, this one included, so the name is the only thing that says
// which printing of the game a product actually is.
var languageWords = map[string]string{
	"japanese": "Japanese",
	"english":  "English",
}

// languageSaid matches the language a label names, on its own or in front
// of the word it qualifies: "Japanese Version 3rd Anniversary Set",
// "Japanese 1st Anniversary Set". nameLanguage publishes it, and a label
// that names it too says twice what one field says once.
var languageSaid = regexp.MustCompile(`(?i)\b(?:japanese|english)(?:\s+version)?\b`)

// seasonPrefixRe matches the season or championship a label opens with,
// which the catalog repeats in front of every pack that season handed out:
// "CS 2023 Celebration Pack", "CS 25-26 Event Pack", "Championship 2024 Top
// Player Pack". The season is one label and what it handed out is another,
// and saying them apart is what lets a celebration pack of one season read
// as the same kind of thing as another's.
var seasonPrefixRe = regexp.MustCompile(`(?i)^((?:(?:january|february|march|april|may|june|july|august|september|october|november|december)\s+)?(?:CS ?\d{4}|CS ?\d{2}-\d{2}|Championship ?\d{4}|Championship ?\d{2}-\d{2}|(?:19|20)\d{2}))\s+(\S.*)$`)

// wrappedRe matches a label the catalog wraps in dashes, which are how it
// sets an edition off rather than anything the label says: "Official
// Playmat -Limited Edition Vol. 3-" beside an unwrapped Vol.4 and Vol.5.
var wrappedRe = regexp.MustCompile(`^(.*?)\s*-(.+)-$`)

// spelledVolume writes the word the catalog abbreviates almost everywhere:
// one "Double Pack Set Volume 2" against ten "Vol." siblings.
var spelledVolume = strings.NewReplacer("Volume ", "Vol. ")

// spacedRe puts the space back where the catalog drops it. It writes the
// season both "CS 25-26" and "CS26-27", and the volume both "Vol. 3" and
// "Vol.4", so one family reads two ways for no reason either spelling
// gives.
var (
	spacedSeasonRe = regexp.MustCompile(`(?i)\bCS(\d)`)
	spacedVolumeRe = regexp.MustCompile(`(?i)\b(Vol\.)(\d)`)
)

// verSuffixRe matches a label saying it is a version of itself. "Event Pack
// Finalist Ver." is the pack "Event Pack" handed to a finalist, and the
// four letters on the end are all that kept the placing from being read as
// one. It is taken off the token and left on the variant, which is the
// wording a listing arrives in.
var verSuffixRe = regexp.MustCompile(`(?i)\s+ver\.?$`)

// foldQualKey is what two spellings of one label have in common: no case,
// no punctuation, and no plural on the end. One "s" is dropped rather than
// every trailing one, and only from a word long enough to still be a word
// without it. A label that is punctuation and nothing else keeps itself,
// so two of those are not read as one.
func foldQualKey(qual string) string {
	var key strings.Builder
	for _, r := range strings.ToLower(qual) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			key.WriteRune(r)
		}
	}
	out := key.String()
	if out == "" {
		return strings.ToLower(strings.TrimSpace(qual))
	}
	if len(out) > 3 && strings.HasSuffix(out, "s") {
		out = out[:len(out)-1]
	}
	return out
}

// promoTypesOf folds a printing's labels to the spelling the matcher
// declares tags in. Every label this build keeps is one: the treatments
// ("Alternate Art", "Parallel", "Jolly Roger Foil"), the shelves a printing
// was handed out on ("Judge Pack Vol. 2", "Premium Card Collection -Best
// Selection Vol. 1-") and the placings ("Online Regional 2023 Winner").
// What is not one is already in the name, where the epithet election puts
// it - "Miss Doublefinger (Zala)" is the character, not a promotion.
// donSubjects are what a DON!! card pictures. Every DON!! card is named
// "DON!! Card", so the label on one either says what is drawn on it or how
// it was made and handed out, and only the second kind is a promotion:
// "Gold" and "Silhouette" and "Double Pack Set Vol. 5" stay, and the
// character, the crew and the scene leave for the variant they already are.
//
// Named rather than read off the data, because nothing in the data tells a
// subject from a treatment. A label appearing only on DON!! cards is one or
// the other - that test takes "Double Pack Set Vol. 5" along with Luffy -
// and a label shared with a normal card is a treatment only by luck: "Gold"
// escapes it on the strength of one Buggy carrying it.
var donSubjects = map[string]bool{
	"2y":                        true,
	"ace":                       true,
	"ace, luffy, sabo":          true,
	"big mom":                   true,
	"blackbeard":                true,
	"blue":                      true,
	"bonney":                    true,
	"chopper":                   true,
	"dragon":                    true,
	"elbaph luffy":              true,
	"enel lightning":            true,
	"eustass \"captain\" kid":   true,
	"gear 4 luffy":              true,
	"gear 5":                    true,
	"gear5 luffy":               true,
	"green":                     true,
	"iceberg":                   true,
	"ivankov":                   true,
	"ivankov & sanji":           true,
	"katakuri":                  true,
	"luffy":                     true,
	"luffy and ace":             true,
	"luffy and loki":            true,
	"luffy vs. crocodile":       true,
	"mihawk & zoro":             true,
	"netflix tony tony.chopper": true,
	"oden":                      true,
	"op-op fruit":               true,
	"orange":                    true,
	"pink":                      true,
	"pudding":                   true,
	"purple":                    true,
	"red":                       true,
	"reiju":                     true,
	"robin":                     true,
	"rocks":                     true,
	"rosinante":                 true,
	"sky island map":            true,
	"teach":                     true,
	"teal":                      true,
	"the four emperors":         true,
	"trafalgar law, eustass kid and monkey.d.luffy": true,
	"vivi":         true,
	"whitebeard":   true,
	"world united": true,
	"yellow":       true,
	"young luffy":  true,
	"zoro":         true,
}

// donCardName is what every DON!! card is called, which is why the label
// on one has to say which it is.
const donCardName = "DON!! Card"

// placings are how a prize card says where its holder finished. They are
// named rather than read off the labels already written, because any label
// can end in a word some other label is: "One Piece Film Red" ends in a
// "Red" this build writes elsewhere, and cutting there would make a movie
// into a colour.
var placings = map[string]bool{
	"winner": true, "finalist": true, "participant": true,
	"1st place": true, "2nd place": true, "3rd place": true,
	"4th place": true, "champion": true, "runner-up": true,
	// How far a regional's holder got, which the catalog writes on the end
	// like any other placing: "Regionals Season 2 Top 16".
	"top 4": true, "top 8": true, "top 16": true, "top 32": true,
	"top 64": true, "top 128": true,
}

// qualSpellings name a label the catalog spells two ways that fold to two
// keys, so nothing else here can pair them.
var qualSpellings = map[string]string{
	"finals":        "World Final",
	"serial number": "Serial Numbered",
	// A treatment named with and without the surface it is on, and a pack
	// named with and without its plural.
	"textured":           "Textured Foil",
	"top players pack":   "Top Player Pack",
	"psa magazine promo": "PSA Magazine",
	// "Promo Reprint" on a promo reprinted into a starter deck. The
	// reprinting is the label; that it was a promo is what the set it came
	// from says.
	"promo reprint": "Reprint",
}

// qualSplits name a label the catalog wrote as one and every other card
// writes as two. A dash is not enough on its own to say so: this category
// hangs deck ranges off one - "Beginners Deck Party [ST-23] - [ST-28]",
// "ST15 - ST20 Release Event Pack" - and cutting there would halve a range.
var qualSplits = map[string][]string{
	"leader pack - live action":                  {"Leader Pack", "Live Action"},
	"pre-errata demo deck":                       {"Pre-Errata", "Demo Deck"},
	"3rd anniversary tournament 3 brothers pack": {"3rd Anniversary Tournament", "3 Brothers Pack"},
}

// cutPlacing splits a label that ends in one, longest tail first so "2nd
// Place" is taken whole rather than "Place" alone.
func cutPlacing(qual string) (string, string, bool) {
	fields := strings.Fields(qual)
	for i := 1; i < len(fields); i++ {
		head := strings.Join(fields[:i], " ")
		tail := strings.Join(fields[i:], " ")
		if placings[strings.ToLower(tail)] {
			return head, tail, true
		}
	}
	return qual, "", false
}

// respellQual writes a label the one way this datastore spells it, where
// the catalog spells it several.
func respellQual(qual string) string {
	qual = spelledVolume.Replace(strings.TrimSpace(qual))
	qual = spacedSeasonRe.ReplaceAllString(qual, "CS $1")
	qual = spacedVolumeRe.ReplaceAllString(qual, "$1 $2")
	if m := wrappedRe.FindStringSubmatch(qual); m != nil {
		qual = strings.TrimSpace(m[1] + " " + m[2])
	}
	return strings.Join(strings.Fields(qual), " ")
}

var (
	// When a promotion ran, in the four ways this catalog writes it: a
	// year, a season across two of them, a month beside a year, and a
	// quarter.
	runYear    = regexp.MustCompile(`\b(?:19|20)[0-9]{2}\b`)
	runSeason  = regexp.MustCompile(`\b[0-9]{2}\s*-\s*[0-9]{2}\b`)
	runMonth   = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\b`)
	runQuarter = regexp.MustCompile(`(?i)\b[a-z]{3}\.?\s*-\s*[a-z]{3}\.?\b`)
	// Which instalment of a promotion this is, in the four ways this
	// catalog writes it: a volume, a numbered season, an ordinal in front
	// of what it counts, and a bare number on the end. "Championship 2024
	// Finalist Card Set Vol. 2" and "CS 25-26 Finalist Card Set 1" are the
	// same card set counted two ways.
	runVolume   = regexp.MustCompile(`(?i)\s*\bvol\.?\s*([0-9]{1,2})\b`)
	runSeasonNo = regexp.MustCompile(`(?i)\bseason\s*([0-9]{1,2})\b`)
	runOrdinal  = regexp.MustCompile(`(?i)\b([0-9]{1,2}(?:st|nd|rd|th))\b`)
	runTrailNo  = regexp.MustCompile(`\s+([0-9]{1,2})$`)
	// The set a release event released, which the entry's own set code
	// already says: "OP10 Release Event", "OP-03 Pre-Release Tournament",
	// "ST15 - ST20 Release Event Pack".
	runSetCode = regexp.MustCompile(`(?i)\b(?:op|st|eb|prb)-?[0-9]{1,2}\b(?:\s*-\s*(?:op|st|eb|prb)-?[0-9]{1,2}\b)?`)
)

var months = map[string]int{
	"january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6,
	"july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
}

// promoWhen takes a label apart into the two things below it: what is left
// once the date and the count of which running this is are gone, which is
// the promotion's own name, and the label with only the date gone, which is
// what the mark is measured against.
//
// "Tournament Pack 2025 Vol. 3" and "Tournament Pack Vol. 7" are one
// promotion run eleven times and written eleven ways.
func promoWhen(label string) (rest, dated, year, month string) {
	dated = label
	if m := runMonth.FindString(dated); m != "" && runYear.MatchString(dated) {
		month, dated = strings.ToLower(m), strings.Replace(dated, m, " ", 1)
	}
	if m := runQuarter.FindString(dated); m != "" && runYear.MatchString(dated) {
		dated = strings.Replace(dated, m, " ", 1)
	}
	if m := runYear.FindString(dated); m != "" {
		year, dated = m, strings.Replace(dated, m, " ", 1)
	}
	dated = strings.Join(strings.Fields(dated), " ")
	rest = dated
	for _, counted := range []*regexp.Regexp{runSeason, runVolume, runSeasonNo, runOrdinal} {
		rest = counted.ReplaceAllString(rest, " ")
	}
	rest = strings.Join(strings.Fields(rest), " ")
	rest = strings.TrimSpace(runTrailNo.ReplaceAllString(rest, ""))
	return strings.Join(strings.Fields(rest), " "), dated, year, month
}

// markLeft is what a label said that its promo type did not take: the count
// of which running this is, and the words the fold dropped off the end.
//
// It is the label's own words in the label's own order, minus the ones the
// token took, because the mark is a fact and not a wording this build
// invents. "Premium Card Collection -Best Selection Vol. 1-" is a token
// naming the collection and a mark saying which issue, and a listing naming
// the issue is naming that printing and no other.
func markLeft(dated, stem string) string {
	taken := map[string]int{}
	for _, word := range strings.Fields(stem) {
		taken[word]++
	}
	var kept []string
	for _, word := range strings.Fields(dated) {
		if taken[word] > 0 {
			taken[word]--
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

// promoReleaseDate is the date a year states, published only where the set
// does not already state it. "One Piece Promotion Cards" is one set holding
// everything handed out since 2022 and the shelf is dated 2022-09-30, so the
// year a label states is the only date most of these printings have.
func promoReleaseDate(year, month, setDate string) string {
	if year == "" {
		return ""
	}
	if len(setDate) >= 4 && setDate[:4] == year && month == "" {
		return ""
	}
	at := months[month]
	if at == 0 {
		at = 1
	}
	return fmt.Sprintf("%s-%02d-01", year, at)
}

// datePrintings turns the year a label stated into the date it is, where the
// set does not already state it, and clears what noteWhen stashed. See
// promoReleaseDate.
func datePrintings(cards []any, sets map[string]any) int {
	var dated int
	for _, raw := range cards {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		year, _ := item["_year"].(string)
		month, _ := item["_month"].(string)
		delete(item, "_year")
		delete(item, "_month")
		setDate := ""
		if held, ok := sets[fmt.Sprint(item["setCode"])].(map[string]any); ok {
			setDate = fmt.Sprint(held["releaseDate"])
		}
		if date := promoReleaseDate(year, month, setDate); date != "" {
			item["originalReleaseDate"] = date
			dated++
		}
	}
	return dated
}

// tidyLabel drops what a strip leaves behind: the brackets that held a set
// code, and the dash between two of them. "Beginners Deck Party [ST-23] -
// [ST-28] Participation Pack" comes out of the set-code strip as "Beginners
// Deck Party [] - [] Participation Pack", and a name is not punctuation.
func tidyLabel(label string) string {
	var kept []string
	for _, field := range strings.Fields(label) {
		if emit.PromoSlug(field) == "" {
			continue
		}
		kept = append(kept, field)
	}
	return strings.Join(kept, " ")
}

// epithetKey is a name or a qualifier reduced to what the catalog and the
// list can be compared by: the list writes "Mr.2.Bon.Kurei(Bentham)" with
// no space before the bracket and dots for the spaces.
func epithetKey(s string) string {
	return strings.NewReplacer(" ", "", ".", "").Replace(strings.ToLower(s))
}

// packSeam is a label naming an occasion and the pack or set it handed out:
// "Store Championship Participation Pack" is a store championship and a
// participation pack, "Offline Regional Finalist Card Set" an offline
// regional and a finalist card set, and a query naming either should reach
// the printing. Cut at the seam, the two are two tokens.
var packSeam = regexp.MustCompile(`^(.+?)\s+((?:participation|participant|winner|campaign|celebration|event|finalist|champion|top players?) (?:pack|card set|card)|trophy card|textured chase promo|chase promo|textured foil)$`)

// releaseEvent is the one occasion whose name ends in a word the seam
// reads as a pack's: "Release Event Pack" is the release event's event
// pack, not a release's event pack.
const releaseEvent = "release event"

// shelfHeads are the collections issued several times, whose label names
// the collection and then the issue: "Premium Card Collection Best Selection
// Vol. 1" and "Premium Card Collection Live Action Edition" are one
// collection, and which issue it was is the mark's to say, like any other
// instalment.
var shelfHeads = []string{
	"premium card collection", "official playmat", "sound loader",
	"special goods set", "illustration box", "learn together deck set",
	"ultra deck",
}

// shelfTails are the products a label names after what it collects: "Seven
// Warlords of the Sea Binder Set" is the binder set, and the crew is the
// issue.
var shelfTails = []string{"binder set"}

// gameName is the game's own name in front of a shelf, which every card of
// the game could wear.
const gameName = "one piece card game "

// setNameHeads are the sets' names, lower-cased and longest first, for the
// same stripping the codes get: "Starter Deck 11: Uta Deck Battle" is a deck
// battle, and the deck is the card's own set. Filled in main once the sets
// are known.
var setNameHeads []string

// splitLabel cuts a label at its seam: the promotions it names, and the
// issue it names them for. This replaced a cut at a length, which took a
// promotion's first two words whatever they were and left the rest as a
// mark: "Super Leader Alternate Art" came out as "superleaderalternate" with
// "art" for a mark, and "Online Regional Finalist Card Set" four different
// ways depending on what stood in front of it. A long name that names one
// thing stays one token; the vocabulary check holds a long token against
// the sets, not against a ruler.
func splitLabel(label string) (stems []string, issue string) {
	label = strings.TrimPrefix(label, gameName)
	for _, name := range setNameHeads {
		// A label that is a set's name and nothing else says which
		// product reprinted the card - "Ultra Deck: The Three Captains" on
		// a Romance Dawn card - which is which copy of the number this is,
		// a mark, and promoted nothing.
		if label == name {
			return nil, label
		}
		if strings.HasPrefix(label, name+" ") {
			label = strings.TrimPrefix(label, name+" ")
			break
		}
	}
	if rest := strings.TrimPrefix(label, releaseEvent+" "); rest != label && rest != "" {
		if rest == "pack" {
			rest = "event pack"
		}
		return []string{releaseEvent, rest}, ""
	}
	for _, shelf := range shelfHeads {
		if strings.HasPrefix(label, shelf+" ") {
			return []string{shelf}, strings.TrimPrefix(label, shelf+" ")
		}
	}
	for _, shelf := range shelfTails {
		if strings.HasSuffix(label, " "+shelf) {
			return []string{shelf}, strings.TrimSuffix(label, " "+shelf)
		}
	}
	if m := packSeam.FindStringSubmatch(label); m != nil {
		return []string{m[1], m[2]}, ""
	}
	return []string{label}, ""
}

// noteWhen stashes what promoTypesOf took off the labels, for datePrintings
// to turn into a date once the sets are known and for the mark to be
// written straight out. An instalment and a deck are both marks - which
// running of a promotion, which deck of a set - and no printing carries
// two, so one field holds whichever it has.
func noteWhen(entry map[string]any, year, month, instalment, mark string) {
	if year != "" {
		entry["_year"] = year
	}
	if month != "" {
		entry["_month"] = month
	}
	var marks []string
	for _, held := range []string{mark, instalment} {
		if held != "" {
			marks = append(marks, held)
		}
	}
	if len(marks) > 0 {
		entry["watermark"] = strings.Join(marks, " ")
	}
}

// promoSpellings write a promotion's name where the label the catalog hands
// over is not one: a plural of a name spelled singular everywhere else, a
// placing named as the act rather than the holder, and the game's own name
// in front of its own product. An abbreviation is not one of them - "CS" is
// what a listing says, and folding it left such a listing with one word of
// two to answer on. Only the
// token folds - the variant keeps the catalog's wording, because that is
// the wording a listing arrives in, and a printing whose variant no longer
// holds it comes back as the wrong printing or as none.
var promoSpellings = map[string]string{
	"offline regionals":         "offline regional",
	"one piece anniversary set": "anniversary set",
	"participation":             "participant",
	"regionals":                 "regional",
}

// deckMark matches a label that is a deck and whose deck it is, which says
// which of a set's decks a copy came in rather than what promoted it.
var deckMark = regexp.MustCompile(`(?i)^([a-z][a-z.\-]*)\s+deck$`)

func promoTypesOf(name, rarity string, quals []string, cardNames map[string]bool) (kept, left []string, year, month, instalment, mark string) {
	// The catalog labels are split before they reach here; the
	// hand-carried printings are not, and a label is a label either way.
	expanded := make([]string, 0, len(quals))
	for _, qual := range quals {
		if parts, hand := qualSplits[strings.ToLower(qual)]; hand {
			expanded = append(expanded, parts...)
			continue
		}
		expanded = append(expanded, qual)
	}
	out := make([]string, 0, len(expanded))
	var subjects, instalments []string
	for _, qual := range expanded {
		// A label that is the card's own rarity says what the rarity field
		// says: the nine Treasure Rares are filed at rarity TR and were
		// labelled "TR" as well.
		if strings.EqualFold(qual, rarity) {
			continue
		}
		// Every DON!! card is named "DON!! Card", so the character on one
		// is what tells it from the others - Chopper, Crocodile, Yamato,
		// forty-five of them over 150 printings, every one also the name of
		// a card this datastore carries. Nothing promoted a DON!! card for
		// having Nami on it, so the character stays the variant it is.
		if name == donCardName && (cardNames[epithetKey(qual)] || donSubjects[strings.ToLower(qual)]) {
			// Published as the mark it is, and not held back for the
			// collision guard: every DON!! card is named "DON!! Card" at
			// one number, so the character is the only thing a listing can
			// name to tell one from another, and the ones that happened
			// not to collide said it nowhere at all.
			subjects = append(subjects, strings.ToLower(qual))
			left = append(left, strings.ToLower(qual))
			continue
		}
		label := strings.ToLower(qual)
		// A placing is how far its holder got, and the numbers in "1st
		// Place" and "Top 16" are not the instalment numbers everything
		// below reads them as.
		if placings[label] {
			out = append(out, emit.PromoSlug(label))
			continue
		}
		// A deck is which copy this is, not what promoted it.
		if deckMark.MatchString(label) {
			if mark == "" {
				mark = label
			}
			continue
		}
		// A label saying it is a version of itself, and the placing that
		// hid behind it: "Event Pack Finalist Ver." is the pack "Event
		// Pack" handed to the finalist, and cutPlacing could not see the
		// finalist through the four letters after it.
		var placing string
		if stripped := verSuffixRe.ReplaceAllString(label, ""); stripped != label {
			label = stripped
			if head, tail, split := cutPlacing(label); split {
				label, placing = head, tail
			}
		}
		// The language, which nameLanguage publishes in a field of its own.
		if stripped := strings.Join(strings.Fields(languageSaid.ReplaceAllString(label, " ")), " "); stripped != label && stripped != "" {
			label = stripped
		}
		// The set a release event released, which the entry's own set code
		// already says, and the brackets that held it.
		if stripped := runSetCode.ReplaceAllString(label, " "); stripped != label {
			if tidied := tidyLabel(stripped); tidied != "" {
				label = tidied
			}
		}
		rest, dated, when, at := promoWhen(label)
		if when != "" && year == "" {
			year, month = when, at
		}
		if rest == "" {
			continue
		}
		stems, _ := splitLabel(rest)
		// Everything the tokens did not take is the mark: the count of
		// which running this is, and the issue of a collection issued more
		// than once. Setting the second aside for the collision guard alone
		// published nothing for the printings that did not collide, so a
		// listing naming the issue had no issue to name.
		if of := markLeft(dated, strings.Join(stems, " ")); of != "" {
			instalments = append(instalments, of)
		}
		for _, stem := range stems {
			if spelled, named := promoSpellings[stem]; named {
				stem = spelled
			}
			if slug := emit.PromoSlug(stem); slug != "" && !slices.Contains(out, slug) {
				out = append(out, slug)
			}
		}
		if placing != "" {
			out = append(out, emit.PromoSlug(placing))
		}
	}
	return out, left, year, month, strings.Join(append(subjects, instalments...), " "), mark
}

// nameLanguage names the language a product's qualifiers call it out as,
// empty when they name none.
func nameLanguage(quals []string) string {
	for _, qual := range quals {
		for _, word := range strings.Fields(strings.ToLower(qual)) {
			language, found := languageWords[word]
			if found {
				return language
			}
		}
	}
	return ""
}

// numberFor is the collector number a product is filed under: the catalog's
// own Number, or the product's card type where the game numbers nothing.
// A product carrying neither has nothing to be told apart by and stops the
// build rather than being dropped from it.
func numberFor(product tcgplayer.Product) string {
	num := product.Extended("Number")
	if num != "" {
		return num
	}
	cardType := product.Extended("CardType")
	if strings.EqualFold(cardType, donCardType) {
		return donNumber
	}
	if cardType == "" {
		log.Fatalf("%q (%d) carries neither a collector number nor a card type",
			product.Name, product.ProductID)
	}
	return strings.ToUpper(cardType)
}

// single is one card product, its name split into the base name, the
// parenthetical qualifiers, and the collector number.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []string

	// language is what the product's own name calls the printing out as,
	// read before the election below moves a qualifier into the name.
	language string
}

// decorations strips the collector number worn as decoration: a dash
// suffix ("Yamato - OP16-098") and the parenthetical forms ("(003)",
// "(OP01-003)").
// rawNames repair a product name the catalog wrote in a shape nothing can
// read, keyed by the product id it never reuses. One name in 7,100 carries
// a bare number that is not the card's: 788 write the number as three
// digits and this one writes four.
var rawNames = map[int]string{
	// "Tony Tony.Chopper (0070) (Parallel)" is OP08-007, and the extra
	// zero is the only thing keeping the number from being read as one.
	558030: "Tony Tony.Chopper (007) (Parallel)",
}

func decompose(p tcgplayer.Product, num string) single {
	name := p.Name
	if repaired, hand := rawNames[p.ProductID]; hand {
		name = repaired
	}
	name = strings.ReplaceAll(name, " - "+num, "")

	var quals []string
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if bareNumRe.MatchString(q) || strings.EqualFold(q, num) {
			return ""
		}
		quals = append(quals, respellQual(q))
		return ""
	})
	// The bracketed tail is the placement a prize card was handed out for
	// - "[Winner]", "[Finalist]", "[Participant]" - and a placement is not
	// part of a card's name: the card is Trafalgar Law however the player
	// holding it finished. It is peeled after the parentheses so the label
	// reads the way the event's own does, the event and then the placing
	// ("Online Regional 2023 Finalist"), which is the shape the copies
	// already carrying their placement as a variant wear.
	name = bracketRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "[]"))
		if q == "" {
			return ""
		}
		quals = append(quals, respellQual(q))
		return ""
	})
	// A name never ends in the character that joined it to what has just
	// been peeled off. The catalog writes one that does - "Dracule Mihawk
	// - (CS 26-27 Regionals Season 1)" - and the dash outlived the
	// parenthesis it was holding on to.
	base := strings.Join(strings.Fields(name), " ")
	base = strings.TrimSpace(strings.TrimRight(base, " -,/&"))

	return single{
		product:  p,
		number:   num,
		baseName: base,
		quals:    quals,
		language: nameLanguage(quals),
	}
}

// handCarried is a printing CardTrader models and neither of this build's
// own sources does, which collectors and marketplaces treat as its own
// object. The catalog does not sell it, so it has no product id and no
// price on TCGplayer, and Bandai's list does not distinguish it either.
//
// Most are errata: an early print run whose rules text was corrected later.
// An errata is a correction to a card and not a new printing, so
// punk-records files OP01-049 once. The rest are the other ways a One Piece
// card is reprinted without becoming a new card - a Revision Pack reissue,
// which prints a foil card without its foil, and a reprint whose text boxes
// sit apart from the original's.
//
// It is therefore hand-carried, the way the release dates neither source
// can answer for are. The entry is the base printing's in every respect but
// three: the errata generation as its variant label, the Cardmarket product
// where there is one, and an id minted from CardTrader's blueprint, which
// is what names the printing. The collector number stays the card's own -
// CardTrader spells the generation into the number ("OP01-049α") and this
// datastore spells it into the variant, which is where a printing's
// distinctions live here.
//
// The list is read off CardTrader's own blueprints - every one whose
// version names an errata - and not off a hand-picked sample, because a
// sample of a shelf this irregular is how printings go missing: an earlier
// 25-row list of these held 21 of the 62 the corpus carries.
//
// Two kinds are left out. A blueprint with a TCGplayer id is a card the
// catalog already sells, so the entry built from that product names it and
// a second one here would be the same card twice - this is what the "1st
// Print Errata" printings are, and all 13 of them are the catalog's. A
// blueprint with no collector number is a sealed product, not a printing.
//
// The alternate art is read from the version wording and never from the
// number, because CardTrader's trailing letters do not mean one thing:
// OP01-064e is the plain printing and OP01-064a the alternate art, while
// OP01-070e is the alternate art and OP01-070ae the plain one. The wording
// says "Alternate Art" or it does not.
type handCarried struct {
	// number is the card's collector number.
	number string
	// parent is the variant label of the printing this is an errata of,
	// empty for the plain one, and is what the entry reads its finish,
	// artwork and rarity from. CardTrader calls the alternate art
	// "Alternate Art" where this category's catalog calls it "Parallel",
	// and the printing wears its own artwork and finish, so an errata
	// filed against the base printing would carry the wrong picture.
	parent string
	// set is the set the printing is sold on, empty where that is the
	// parent printing's own. A reissue is the parent's card printed onto
	// another shelf: a Revision Pack card is an OP01 card sold in the
	// revision pack, and the catalog gives that pack a set of its own.
	// The parent is still read from the number's set, because that is
	// where the printing being reissued lives.
	set string
	// label names what this printing is, and joins the parent label to
	// make the variant a query carries: the pre-errata of a parallel
	// printing is a "Parallel Alpha Pre-Errata".
	label string
	// finish overrides the parent printing's, empty where it is the
	// parent's own. A Revision Pack reissue is the one thing here that is
	// not the parent in every physical respect: it prints a foil card
	// without the foil, which is the whole of what distinguishes it.
	finish string
	// cardmarket is the product Cardmarket sells it as, 0 where it sells
	// none: with no TCGplayer id, this is the only id it can be priced by.
	cardmarket int
	// blueprint is CardTrader's own id for the printing, which the entry's
	// uuid is minted from so it cannot collide with a product id.
	blueprint int
}

var handCarriedPrintings = []handCarried{
	{"OP01-002", "", "", "Alpha Pre-Errata", "", 0, 277476},
	{"OP01-002", "", "", "Beta Pre-Errata", "", 0, 277470},
	{"OP01-002", "Parallel", "", "Alpha Pre-Errata", "", 755413, 277478},
	{"OP01-002", "Parallel", "", "Beta Pre-Errata", "", 0, 277477},
	{"OP01-003", "", "", "Pre-Errata", "", 755414, 277276},
	{"OP01-003", "", "", "Pre-Errata Demo Deck", "", 873904, 374952},
	{"OP01-005", "", "OP-RP", "", "Normal", 719315, 260377},
	{"OP01-006", "", "", "Pre-Errata", "", 755415, 277513},
	{"OP01-013", "", "OP-RP", "", "Normal", 719316, 260772},
	{"OP01-016", "", "", "Alpha Pre-Errata", "", 768142, 277561},
	{"OP01-016", "Parallel", "", "Alpha Pre-Errata", "", 755416, 277442},
	{"OP01-016", "Parallel", "", "Beta Pre-Errata", "", 0, 277443},
	{"OP01-016", "", "OP-RP", "", "Normal", 719314, 260791},
	{"OP01-025", "", "", "Pre-Errata", "", 865609, 366332},
	{"OP01-026", "", "", "Pre-Errata", "", 768145, 319050},
	{"OP01-040", "Parallel", "", "Pre-Errata", "", 768200, 319051},
	{"OP01-047", "", "", "Alpha Pre-Errata", "", 755417, 277454},
	{"OP01-047", "", "", "Beta Pre-Errata", "", 0, 277455},
	{"OP01-047", "Parallel", "", "Alpha Pre-Errata", "", 755418, 277403},
	{"OP01-047", "Parallel", "", "Beta Pre-Errata", "", 0, 277451},
	{"OP01-049", "", "", "Alpha Pre-Errata", "", 0, 277562},
	{"OP01-049", "", "", "Beta Pre-Errata", "", 0, 277563},
	{"OP01-051", "", "", "Pre-Errata", "", 0, 244690},
	{"OP01-051", "Parallel", "", "Pre-Errata", "", 0, 244691},
	{"OP01-061", "", "", "Pre-Errata", "", 0, 277515},
	{"OP01-061", "Parallel", "", "Pre-Errata", "", 755419, 277514},
	{"OP01-064", "", "", "Pre-Errata", "", 0, 244443},
	{"OP01-064", "Box Topper", "", "Pre-Errata", "", 755420, 244685},
	{"OP01-069", "", "", "Pre-Errata", "", 0, 244686},
	{"OP01-070", "", "", "Pre-Errata", "", 755422, 244688},
	{"OP01-070", "Parallel", "", "Pre-Errata", "", 755421, 244687},
	{"OP01-071", "", "", "Pre-Errata", "", 0, 244689},
	{"OP01-087", "", "", "Alpha Pre-Errata", "", 0, 277564},
	{"OP01-087", "", "", "Beta Pre-Errata", "", 0, 277565},
	{"OP01-093", "", "", "Pre-Errata", "", 768185, 277566},
	{"OP01-093", "Parallel", "", "Pre-Errata", "", 755423, 277545},
	{"OP01-096", "", "", "Pre-Errata", "", 0, 277558},
	{"OP01-096", "Parallel", "", "Pre-Errata", "", 755424, 277557},
	{"OP01-097", "", "", "Pre-Errata", "", 0, 277560},
	{"OP01-097", "Parallel", "", "Pre-Errata", "", 755425, 277559},
	{"OP01-112", "", "", "Pre-Errata", "", 0, 255423},
	{"OP01-119", "", "", "Pre-Errata", "", 768197, 323775},
	{"OP03-047", "", "", "Pre-Errata", "", 898777, 272148},
	{"OP03-054", "", "", "Pre-Errata", "", 0, 272147},
	{"OP05-032", "", "", "Pre-Errata", "", 0, 272131},
	{"OP06-023", "", "PRB-01", "Reprint", "", 799353, 310301},
	{"OP13-077", "", "", "Pre-Errata", "", 0, 354998},
	{"OP13-119", "", "", "Pre-Errata", "", 857345, 354999},
	{"ST03-009", "", "", "Alpha Pre-Errata", "", 0, 320712},
}

// nonCodeRe matches the runs a set code cannot carry.
var nonCodeRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// setCodeOf reduces a catalog abbreviation to what a search query can carry.
// A set code is typed after "is:", and a query is split on whitespace before
// a filter ever sees it, so a code holding a space cannot be asked for:
// "is:OP11 RE" reaches the filter as "is:OP11". Every run of anything but a
// letter or a digit becomes one dash, and the ends are trimmed of them.
// idStem spells a collector number for the inside of a uuid: a number can
// carry the set total it is printed with ("1/1000"), and a slash in a uuid
// is a path separator wherever one is written down. Every run of anything
// but a letter or a digit becomes one dash.
func idStem(number string) string {
	return strings.ToLower(strings.Trim(nonCodeRe.ReplaceAllString(number, "-"), "-"))
}

func setCodeOf(abbreviation string) string {
	return strings.Trim(nonCodeRe.ReplaceAllString(abbreviation, "-"), "-")
}

// packKey reduces a Bandai pack label and a TCGplayer group abbreviation to
// what the two spell alike: Bandai writes "OP-01" where the catalog writes
// "OP01". A group the catalog qualifies further - "OP02 PRE" for the
// pre-release of a set - keeps the qualifier and so matches no pack, which
// is right: it is a different product handing the cards out.
func packKey(s string) string {
	return strings.ToUpper(nonCodeRe.ReplaceAllString(s, ""))
}

// packLabels names the pack Bandai labels differently from the group
// TCGplayer files the same cards under: the fourteenth booster shipped with
// the fourth extra booster and the list labels the one pack "OP14-EB04"
// where the catalog groups them apart, and the catalog files both Heroines
// Editions under one group where the list labels the first "EB-03". A row
// is reported the build it stops doing anything.
var packLabels = map[string]string{
	"OP14":     "OP14-EB04",
	"EB-03-04": "EB-03",
}

// isReprint reports whether a product is the reprint printing of its
// number, which Bandai suffixes _rN where every other variant is _pN.
func isReprint(s *single) bool {
	for _, q := range s.quals {
		if strings.EqualFold(q, "Reprint") {
			return true
		}
	}
	return false
}

// groupKey is a catalog group's set code the way a pack label spells it,
// through packLabels where the two sources spell one set two ways.
func groupKey(group tcgplayer.Group) string {
	if label, aliased := packLabels[group.Abbreviation]; aliased {
		return packKey(label)
	}
	return packKey(group.Abbreviation)
}

// isPromoGroup reports whether a catalog group hands its cards out rather
// than selling them in packs of its own.
//
// The matcher has been reading this off the set name because the datastore
// typed no set - "promotion cards" or "pre-release", in a function whose
// comment says so - and this is that same test, moved to the side that can
// see the catalog. Two families answer to it: the promo set, which names
// itself, and the pre-release sets, which hand out stamped copies ahead of
// a release. Matching the name rather than the code keeps the Premium
// Booster sets out, whose codes begin "PRB" but which are an ordinary
// product.
//
// The event distributions this leaves untyped - the Release Event and
// Anniversary Tournament cards, 21 groups over 1,600 printings - hand their
// cards out too, and typing them is a change to what the matcher believes
// rather than a change to where the belief is written. That is a decision,
// not a port, and it is not made here.
func isPromoGroup(group tcgplayer.Group) bool {
	lower := strings.ToLower(group.Name)
	return strings.Contains(lower, "promotion cards") || strings.Contains(lower, "pre-release")
}

// setCodes assigns every group a unique, non-empty set code. Abbreviations
// repeat across groups in this category the way they do in every other one
// — a set beside the promo group that hands its cards out, a reissue beside
// the original — and a map keyed on the bare abbreviation silently folded
// the later group onto the earlier, dropping its name and release date and
// filing both groups' cards under one set. Codes are claimed in group-id
// order, so the group that claimed one keeps it bare and only the later
// arrival is marked: a set code then depends on the groups that came before
// it and never on the ones that come after, and an existing set keeps its
// code the day TCGplayer files a new group under an abbreviation it already
// uses. A blank abbreviation gets a code minted from the group id. Every
// repair is logged, because none of it is the catalog's own identity.
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

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 68 (required)")
	punkCards := flag.String("punk-cards", punkCardsURL, "punk-records cards_by_id file, path or URL")
	punkPacks := flag.String("punk-packs", punkPacksURL, "punk-records packs file, path or URL")
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
	if catalog.Category.CategoryID != onepieceCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, onepieceCategory)
	}

	punkData, err := emit.Fetch(*punkCards)
	if err != nil {
		log.Fatalln("punk-records:", err)
	}
	var punk map[string]punkCard
	if err := json.Unmarshal(punkData, &punk); err != nil {
		log.Fatalln("punk-records:", err)
	}
	// Bandai hangs two kinds of printing suffix off a collector number:
	// "_pN" for the parallel arts and "_rN" for the reprints. Cutting only
	// at "_p" left every reprint keyed under a number of its own, where it
	// matched no catalog product and, worse, went missing from the count
	// its real number is aligned by - so a number printed both ways lost
	// the annotation for all of its printings. A One Piece collector number
	// holds no underscore of its own, so the first one always starts the
	// suffix.
	packsData, err := emit.Fetch(*punkPacks)
	if err != nil {
		log.Fatalln("punk-records packs:", err)
	}
	var packs map[string]punkPack
	if err := json.Unmarshal(packsData, &packs); err != nil {
		log.Fatalln("punk-records packs:", err)
	}
	// The set code each pack hands its cards out under, as a catalog group
	// abbreviation would spell it. A pack Bandai gives no label - the two
	// promotional ones - maps to nothing and pins nothing.
	packSet := map[string]string{}
	packKeys := map[string]bool{}
	for id, pack := range packs {
		if key := packKey(pack.TitleParts.Label); key != "" {
			packSet[id] = key
			packKeys[key] = true
		}
	}
	for abbreviation, label := range packLabels {
		switch {
		case !packKeys[packKey(label)]:
			log.Printf("pack labels: no pack is labelled %q any more; the row for group %q does nothing", label, abbreviation)
		case packKeys[packKey(abbreviation)]:
			log.Printf("pack labels: a pack is labelled %q now; the row aliasing it to %q does nothing", abbreviation, label)
		}
	}

	punkByNumber := map[string][]string{}
	// What the list calls the card at each number, the witness for which
	// parenthetical is the card's name.
	punkNames := map[string][]string{}
	for id, card := range punk {
		base, _, _ := strings.Cut(id, "_")
		punkByNumber[base] = append(punkByNumber[base], id)
		if card.Name != "" && !slices.Contains(punkNames[base], card.Name) {
			punkNames[base] = append(punkNames[base], card.Name)
		}
	}
	for _, names := range punkNames {
		sort.Strings(names)
	}
	for _, ids := range punkByNumber {
		sort.Strings(ids)
	}
	log.Printf("catalog: %d groups, %d products; punk-records: %d printings over %d numbers",
		len(catalog.Groups), len(catalog.Products), len(punk), len(punkByNumber))

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
			// Every card product the catalog has ever carried prices at
			// least one sku, and a product with none has no printing to
			// file an entry under: stop rather than drop it.
			log.Fatalf("no sku printing: %q (%d) has no entry to carry it",
				product.Name, product.ProductID)
		}
		if product.Extended("Number") == "" {
			unnumbered++
		}
		singles = append(singles, decompose(product, numberFor(product)))
	}
	log.Printf("singles: %d kept (%d filed under a card type for want of a number)",
		len(singles), unnumbered)

	// Per collector number: a qualifier every product of the number carries
	// is part of the name (the "(Bentham)" epithets), not a variant. A
	// number with a single product cannot make that call alone, so the
	// epithets learned from the multi-product numbers decide for it — the
	// same epithet decorates the character's every printing. The DON!!
	// bucket is the one holding unrelated cards rather than one card's
	// printings, and it holds undecorated ones, so the rule finds nothing
	// common there and hands every qualifier to the variant label.
	byNumber := map[string][]*single{}
	for i := range singles {
		byNumber[singles[i].number] = append(byNumber[singles[i].number], &singles[i])
	}
	// Which parentheticals are part of a card's name is the list's to say.
	// Bandai names the card "Miss Doublefinger(Zala)" and "Mr.2.Bon.Kurei
	// (Bentham)", and the catalog decorates the same products with the same
	// epithet, so a qualifier that appears in parentheses in the list's
	// name for the number is the card's name and any other is the
	// printing's variant. The vote below, a qualifier every product of a
	// number carries, is kept for the numbers the list does not name. It
	// was right on the epithets and wrong on the promos: a promo number
	// sold plain and as "[Winner]", both wearing the event, was named after
	// the event - "Smoker (Pre-Release)", "Monkey.D.Luffy (4th Anniversary)"
	// - which the list names plainly. Measured before it was written: of
	// the 109 parentheticals in names today, the list witnesses 97, does
	// not know the number of 6, and names the card plainly on the 6 that
	// were wrong.
	witnessed := func(number, qual string) (bool, bool) {
		names := punkNames[number]
		if len(names) == 0 {
			return false, false
		}
		key := "(" + epithetKey(qual) + ")"
		for _, name := range names {
			if strings.Contains(epithetKey(name), key) {
				return true, true
			}
		}
		return false, true
	}
	var witnessedNames, votedNames int
	// What the list has witnessed anywhere is a name everywhere: the same
	// epithet decorates the character's every printing, and the promo
	// numbers the list does not know - P-148 is "Mr.3 (Galdino)" - take
	// the verdict its Romance Dawn printing reached.
	nameParens := map[string]bool{}
	for i := range singles {
		for _, q := range singles[i].quals {
			if named, _ := witnessed(singles[i].number, q); named {
				nameParens[q] = true
			}
		}
	}
	for _, bucket := range byNumber {
		sort.Slice(bucket, func(i, j int) bool {
			return bucket[i].product.ProductID < bucket[j].product.ProductID
		})
		if len(bucket) < 2 || len(punkNames[bucket[0].number]) > 0 {
			continue
		}
		common := map[string]int{}
		for _, s := range bucket {
			for _, q := range s.quals {
				common[q]++
			}
		}
		for q, n := range common {
			if n == len(bucket) {
				nameParens[q] = true
			}
		}
	}
	for _, bucket := range byNumber {
		// Decide before mutating: the membership test must read every
		// product's original qualifiers, not the ones a fold already moved.
		isName := map[string]bool{}
		if len(punkNames[bucket[0].number]) > 0 {
			for _, s := range bucket {
				for _, q := range s.quals {
					if named, _ := witnessed(s.number, q); named {
						isName[q] = true
						witnessedNames++
					}
				}
			}
		} else if len(bucket) < 2 {
			for _, q := range bucket[0].quals {
				isName[q] = nameParens[q]
				if isName[q] {
					votedNames++
				}
			}
		} else {
			common := map[string]int{}
			for _, s := range bucket {
				for _, q := range s.quals {
					common[q]++
				}
			}
			for q, n := range common {
				isName[q] = n == len(bucket)
				if isName[q] {
					votedNames += n
				}
			}
		}
		for _, s := range bucket {
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
	}
	log.Printf("names: %d parentheticals kept in a name on the list's word, %d by the vote where the list names no card",
		witnessedNames, votedNames)

	// A season in front of what it handed out is two labels. The catalog
	// repeats the season on every pack of it, so a celebration pack of one
	// season read as a different thing from another's; apart, the season is
	// a label and so is the pack.
	//
	// A placing on the end is two as well, and this build already writes
	// those apart where the catalog brackets them - "Online Regional 2023"
	// beside "Winner".
	var seasonSplit, placingSplit int
	for i := range singles {
		var out []string
		for _, q := range singles[i].quals {
			if parts, hand := qualSplits[strings.ToLower(q)]; hand {
				out = append(out, parts...)
				continue
			}
			if m := seasonPrefixRe.FindStringSubmatch(q); m != nil {
				out = append(out, strings.TrimSpace(m[1]), strings.TrimSpace(m[2]))
				seasonSplit++
				continue
			}
			out = append(out, q)
		}
		singles[i].quals = out
	}
	for i := range singles {
		var out []string
		for _, q := range singles[i].quals {
			head, tail, split := cutPlacing(q)
			if !split {
				out = append(out, q)
				continue
			}
			out = append(out, head, tail)
			placingSplit++
		}
		singles[i].quals = out
	}
	if seasonSplit > 0 || placingSplit > 0 {
		log.Printf("promo types: %d labels split from the season they open with, %d from a label they end with",
			seasonSplit, placingSplit)
	}

	// One label, one spelling. The catalog writes the same qualifier more
	// than one way - "CS26-27 Regionals Season 1" beside "CS 26-27
	// Regionals Season 1" - and a query naming one misses every printing
	// filed under the other. Spellings fold to the one the catalog uses
	// most, so the winner is the wording a listing is likeliest to arrive
	// in; a tie folds nothing, because there is no preference to read and
	// picking on sort order is not one.
	spellings := map[string]map[string]int{}
	for i := range singles {
		for _, q := range singles[i].quals {
			key := foldQualKey(q)
			if spellings[key] == nil {
				spellings[key] = map[string]int{}
			}
			spellings[key][q]++
		}
	}
	folded := map[string]string{}
	for _, seen := range spellings {
		if len(seen) < 2 {
			continue
		}
		winner, tied := "", false
		for text := range seen {
			switch {
			case winner == "" || seen[text] > seen[winner]:
				winner, tied = text, false
			case seen[text] == seen[winner]:
				tied = true
				if text < winner {
					winner = text
				}
			}
		}
		if tied {
			var texts []string
			for text := range seen {
				texts = append(texts, text)
			}
			sort.Strings(texts)
			log.Printf("promo types: %q are used alike and are left apart", texts)
			continue
		}
		for text := range seen {
			if text != winner {
				folded[text] = winner
				log.Printf("promo types: %q folds into %q (%d against %d)",
					text, winner, seen[text], seen[winner])
			}
		}
	}
	if len(folded) > 0 {
		var refolded int
		for i := range singles {
			for j, q := range singles[i].quals {
				if winner, fold := folded[q]; fold {
					singles[i].quals[j] = winner
					refolded++
				}
			}
		}
		log.Printf("promo types: %d spellings folded away over %d labels", len(folded), refolded)
	}

	// Two labels naming one thing, which the election cannot reach: it
	// compares spellings folding to one key, and these fold to two. The
	// catalog calls the 2024 championship's last round both "World Final"
	// and "Finals", and writes a serialised card both "Serial Number" and
	// "Serial Numbered" three times each - a tie the election leaves alone
	// on purpose.
	var handFixed int
	for i := range singles {
		for j, q := range singles[i].quals {
			if fixed, hand := qualSpellings[strings.ToLower(q)]; hand {
				singles[i].quals[j] = fixed
				handFixed++
			}
		}
	}
	if handFixed > 0 {
		log.Printf("promo types: %d labels named by hand, where frequency had nothing to say", handFixed)
	}

	// Annotate Bandai's printing id where Bandai's own pack says which
	// printing a product is: the product's group and the id's pack must be
	// one set. The plain product of the number's own set is the bare id; a
	// Reprint product is an _rN id and any other variant a _pN, which is
	// what the two suffixes mean; and one product against one id of a kind
	// in one pack is that id. Two products against two ids of a kind in one
	// pack are left unnamed rather than ordered by a product id that says
	// nothing about which is which. A plain product in another set - the
	// starter deck's reprint of a booster card - is a printing of its own
	// and takes an id from that deck's pack, never the bare one. The count
	// is over the English printings alone, because the list is the English
	// one.
	var byBare, byKind, ambiguous, unmatched int
	bandaiIDs := map[int]string{}
	for num, bucket := range byNumber {
		ids := punkByNumber[num]
		if len(ids) == 0 {
			continue
		}
		used := map[string]bool{}
		byGroup := map[int][]*single{}
		var groupIDs []int
		for _, s := range bucket {
			if s.language != "" {
				continue
			}
			if _, seen := byGroup[s.product.GroupID]; !seen {
				groupIDs = append(groupIDs, s.product.GroupID)
			}
			byGroup[s.product.GroupID] = append(byGroup[s.product.GroupID], s)
		}
		sort.Ints(groupIDs)
		for _, groupID := range groupIDs {
			products := byGroup[groupID]
			key := groupKey(groupByID[groupID])
			if key == "" {
				unmatched += len(products)
				continue
			}
			var pool []string
			for _, id := range ids {
				if !used[id] && packSet[punk[id].PackID] == key {
					pool = append(pool, id)
				}
			}
			var plain, rest []*single
			for _, s := range products {
				if len(s.quals) == 0 {
					plain = append(plain, s)
				} else {
					rest = append(rest, s)
				}
			}
			if len(plain) == 1 && sliceContains(pool, num) {
				bandaiIDs[plain[0].product.ProductID] = num
				used[num] = true
				byBare++
			} else {
				rest = append(rest, plain...)
			}
			for _, reprint := range []bool{true, false} {
				var kp []*single
				for _, s := range rest {
					if isReprint(s) == reprint {
						kp = append(kp, s)
					}
				}
				if len(kp) == 0 {
					continue
				}
				var ki []string
				for _, id := range pool {
					if !used[id] && id != num && strings.Contains(id, "_r") == reprint {
						ki = append(ki, id)
					}
				}
				switch {
				case len(kp) == 1 && len(ki) == 1:
					bandaiIDs[kp[0].product.ProductID] = ki[0]
					used[ki[0]] = true
					byKind++
				case len(kp) == len(ki):
					ambiguous += len(kp)
				default:
					unmatched += len(kp)
				}
			}
		}
	}
	log.Printf("bandai ids: %d of %d printings annotated (%d the bare id of their own set, %d the one id of their kind in their pack); %d products face as many ids of their kind in their pack and are left unnamed, %d have none there",
		byBare+byKind, len(singles), byBare, byKind, ambiguous, unmatched)

	// The other direction, which nothing counted before: a Bandai printing
	// whose collector number no card product carries is a card this
	// datastore does not hold at all. The annotation rate above measures
	// only how much of the catalog the official list could name, so a card
	// the game prints and TCGplayer does not sell was invisible - it
	// annotated nothing and showed up as no gap.
	uncarried := map[string]int{}
	var uncarriedPrintings int
	for num, ids := range punkByNumber {
		if len(byNumber[num]) > 0 {
			continue
		}
		uncarried[num] = len(ids)
		uncarriedPrintings += len(ids)
	}
	if len(uncarried) > 0 {
		var numbers []string
		for num := range uncarried {
			numbers = append(numbers, num)
		}
		sort.Strings(numbers)
		log.Printf("punk-records printings this datastore does not carry: %d over %d collector numbers, first is %s",
			uncarriedPrintings, len(numbers), numbers[0])
	} else {
		log.Print("punk-records printings this datastore does not carry: none")
	}

	// Emit. Sets are the catalog groups that hold anything; ids embed the
	// product id so they survive any upstream renumbering. A group with no
	// product is a legacy husk TCGplayer keeps around, and a set nothing
	// references is dead weight in every consumer - it is skipped, not
	// carried. Its code stays claimed above, so no existing set's code
	// moves while it is empty, and the set appears already-coded the day
	// TCGplayer files a product there.
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
	for _, entry := range sets {
		set, _ := entry.(map[string]any)
		if name, _ := set["name"].(string); name != "" {
			setNameHeads = append(setNameHeads, strings.ToLower(name))
		}
	}
	sort.Slice(setNameHeads, func(i, j int) bool {
		if len(setNameHeads[i]) != len(setNameHeads[j]) {
			return len(setNameHeads[i]) > len(setNameHeads[j])
		}
		return setNameHeads[i] < setNameHeads[j]
	})
	log.Printf("promotional sets: %d of %d", promoSets, len(sets))
	if skippedEmpty > 0 {
		log.Printf("sets: %d empty groups hold no product and are skipped", skippedEmpty)
	}
	// The recount: one set per group that holds anything. A code claimed
	// twice would fold two groups onto one entry, and validate cannot see
	// it — the code still resolves for every card naming it, it just
	// names the wrong set.
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

	// The names this datastore carries, which is how a label naming a
	// character is told from one naming a treatment: every character label
	// sits on a DON!! card, and every one of them is a card of its own.
	// Keyed the way the epithets are compared, since the catalog spells one
	// character two ways: "Rocks.D.Xebec" on his cards and "Rocks D. Xebec"
	// on the DON!! card that pictures him, which read as a promotion until
	// the dots and spaces came off both.
	cardNames := map[string]bool{}
	for i := range singles {
		cardNames[epithetKey(singles[i].baseName)] = true
	}

	var cards []any
	var nonEnglish int
	for _, s := range singles {
		group := groupByID[s.product.GroupID]
		productID := s.product.ProductID
		if s.language != "" {
			nonEnglish++
		}
		for _, finish := range printings[productID] {
			suffix := emit.FinishSuffix(finish)
			entry := map[string]any{
				"id":      fmt.Sprintf("%s_%d%s", idStem(s.number), productID, suffix),
				"name":    s.baseName,
				"number":  s.number,
				"setCode": codes[group.GroupID],
				"rarity":  s.product.Extended("Rarity"),
				"color":   s.product.Extended("Color"),
				"type":    s.product.Extended("CardType"),
				"finish":  finish,
				"image":   cardImage(s, bandaiIDs[productID]),
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if len(s.quals) > 0 {
				entry["variant"] = strings.Join(s.quals, " ")
				// The same labels as a list. Joined, "Alternate Art Manga"
				// cannot be read back into the two it holds, and the
				// matcher declares and narrows on them one at a time.
				tags, _, year, month, instalment, mark := promoTypesOf(s.baseName, s.product.Extended("Rarity"), s.quals, cardNames)
				if len(tags) > 0 {
					entry["promoTypes"] = tags
				}
				noteWhen(entry, year, month, instalment, mark)
			}
			if s.language != "" {
				entry["language"] = s.language
			}
			if bandai, found := bandaiIDs[productID]; found {
				entry["bandaiId"] = bandai
			}
			cards = append(cards, entry)
		}
	}

	// The hand-carried pre-errata printings, each read off the base
	// printing its number names.
	//
	// A printing is minted only where nothing already carries its
	// identity. The day a source sells one - TCGplayer has begun
	// modelling these elsewhere - the entry built from that product names
	// the printing, prices through it, and this one would be a second
	// card wearing the same name, number, set and label. So the test is
	// the identity itself and not the number: what the build already
	// names, it leaves alone, and the hand-carried row stands down.
	//
	// The set is read off the base printing rather than the number's
	// prefix, because a set code is claimed in group-id order and the
	// group holding a number need not wear its abbreviation bare.
	carried := map[string]bool{}
	// A printing is looked up by the number and the variant that name it,
	// and the set is settled below.
	printed := map[string][]map[string]any{}
	for _, entry := range cards {
		e, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		variant, _ := e["variant"].(string)
		number, _ := e["number"].(string)
		setCode := fmt.Sprint(e["setCode"])
		carried[fmt.Sprintf("%v|%s|%s|%s", e["name"], number, setCode, variant)] = true
		if _, translated := e["language"]; !translated {
			printed[number+"|"+variant] = append(printed[number+"|"+variant], e)
		}
	}
	var minted, stoodDown int
	for _, printing := range handCarriedPrintings {
		// A number is carried by its own set and by every later one
		// reissuing it - a promo group, a starter deck, an anniversary
		// box. An errata corrects the text the printing carried first,
		// so the parent is the one in the set the number is named for;
		// the reissues came after the correction and were never
		// pre-errata.
		// The number's prefix and the set code it names are spelled
		// differently either side of the dash - "ST03-009" is carried by
		// the set the catalog abbreviates "ST-03" - so the two are
		// compared the way a pack label and an abbreviation already are.
		own := packKey(strings.SplitN(printing.number, "-", 2)[0])
		variant := strings.TrimSpace(printing.parent + " " + printing.label)
		var candidates []map[string]any
		for _, c := range printed[printing.number+"|"+printing.parent] {
			if packKey(fmt.Sprint(c["setCode"])) == own {
				candidates = append(candidates, c)
			}
		}
		if len(candidates) == 0 {
			log.Printf("hand-carried: %s carries no %q printing in %s; %q not carried",
				printing.number, printing.parent, own, variant)
			stoodDown++
			continue
		}
		// One printing, but possibly both its finishes: take the lowest
		// id so the choice does not move between runs.
		sort.Slice(candidates, func(i, j int) bool {
			return fmt.Sprint(candidates[i]["id"]) < fmt.Sprint(candidates[j]["id"])
		})
		src := candidates[0]
		// The parent says where the printing being reissued lives; a row
		// naming its own set says where this one is sold.
		setCode := fmt.Sprint(src["setCode"])
		if printing.set != "" {
			setCode = printing.set
		}
		key := fmt.Sprintf("%v|%s|%s|%s", src["name"], printing.number, setCode, variant)
		if carried[key] {
			log.Printf("hand-carried: %s %q is sold as a product now; the hand-carried one stands down",
				printing.number, variant)
			stoodDown++
			continue
		}
		carried[key] = true

		// A catalog id carries "_<product id>" before its finish suffix,
		// so "_ct<blueprint id>" cannot meet one however the numbers
		// fall, and the suffix stays last the way every other id here
		// spells it.
		finish := fmt.Sprint(src["finish"])
		if printing.finish != "" {
			finish = printing.finish
		}
		entry := map[string]any{
			"id": fmt.Sprintf("%s_ct%d%s", idStem(printing.number),
				printing.blueprint, emit.FinishSuffix(finish)),
			"name":    src["name"],
			"number":  printing.number,
			"setCode": setCode,
			"rarity":  src["rarity"],
			"color":   src["color"],
			"type":    src["type"],
			"finish":  finish,
			"image":   src["image"],
		}
		// A reissue sold on a shelf that names it needs no label saying
		// so: every card in the revision pack set is a revision pack card.
		if variant != "" {
			entry["variant"] = variant
			// The two the variant was joined from, not the words it was
			// joined into: "Alternate Art" is one label and splitting the
			// string would make it two.
			var tags []string
			if printing.parent != "" {
				tags = append(tags, printing.parent)
			}
			if printing.label != "" {
				tags = append(tags, printing.label)
			}
			labels, _, year, month, instalment, mark := promoTypesOf(fmt.Sprint(src["name"]), fmt.Sprint(src["rarity"]), tags, cardNames)
			if len(labels) > 0 {
				entry["promoTypes"] = labels
			}
			noteWhen(entry, year, month, instalment, mark)
		}
		// The blueprint is what this printing was minted from, and until
		// now it was legible only inside the uuid - the one fact about a
		// hand-carried entry that had to be read out of an id rather than
		// off a field. It is published here so nothing has to.
		links := map[string]any{"cardTraderId": printing.blueprint}
		// No TCGplayer product sells it, so the Cardmarket one is the only
		// id it can be priced by; a printing Cardmarket does not sell
		// either carries none, and is carried for resolution alone.
		if printing.cardmarket != 0 {
			links["cardmarketId"] = printing.cardmarket
		}
		entry["externalLinks"] = links
		cards = append(cards, entry)
		minted++
	}
	log.Printf("hand-carried printings: %d carried, %d stood down", minted, stoodDown)

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
	log.Printf("emitting %d sets, %d card entries over %d products (%d not in English), %d sealed",
		len(sets), len(cards), len(singles), nonEnglish, len(sealed))
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	// The id upstream knows this printing by, in the place every other
	// identifier lives. It has been written flat on the entry beside an
	// externalLinks holding only the TCGplayer id, so "which id spaces is
	// this card in" has been two questions rather than one - and three
	// across the eight games, because Riftbound writes the TCGplayer id
	// flat as well. It is written in both places for now: the loader reads
	// the flat one, and the flat one goes when it reads this one instead.
	var linked int
	for _, entry := range cards {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["bandaiId"].(string)
		if id == "" {
			continue
		}
		links, ok := item["externalLinks"].(map[string]any)
		if !ok {
			links = map[string]any{}
			item["externalLinks"] = links
		}
		links["bandaiId"] = id
		linked++
	}
	log.Printf("external links: %d cards carry their bandaiId under externalLinks as well", linked)

	log.Printf("release dates: %d printings dated by a year their label stated, where the set states another",
		datePrintings(cards, sets))

	doc := map[string]any{
		"game":   "onepiece",
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
	// publishing anything: a format drift or a truncated download must
	// fail here, not in every consumer. The types mirror what go-mtgban's
	// mtgmatcher/onepiece reads, duplicated so this repository depends on
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
// and sealed product carrying its identity, every id unique within its
// namespace, no two entries wearing the same identity, every referenced
// set existing, every finish one of the two printing names, and every
// product's entries covering exactly the sku printings the catalog lists
// for it.
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
			Variant       string `json:"variant"`
			Language      string `json:"language"`
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

	if doc.Game != "onepiece" {
		return out, fmt.Errorf("game is %q, not onepiece", doc.Game)
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
	// A query resolves a card by its name, number, set and variant label,
	// never by the id, so two products wearing all four alike are one card
	// to every consumer and would alias each other's prices. The key holds
	// the product id rather than a flag so a product's own Normal and Foil
	// entries pass while two different products never do - keying on the
	// finish instead would wave through exactly the pair this is meant to
	// catch, since most DON!! products carry a single finish. This is what
	// holds the DON!! cards' constant number up: the day a set labels two
	// of them alike, the build says so instead of publishing the pair.
	identities := map[string]string{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		// A hand-carried printing has no TCGplayer product to name it -
		// that is why it is hand-carried - so the product id is required
		// of everything the catalog does sell and of nothing else.
		if card.ID == "" || card.Name == "" || card.Number == "" || card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q", card.Name, card.ID, card.Number)
		}
		if card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) carries no finish at all", card.Name, card.ID)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		// The language is part of the identity for the same reason the
		// variant label is: the matcher narrows on it, so the Japanese
		// printing of a card is not the English one wearing its name.
		identity := strings.Join([]string{
			card.Name, card.Number, card.SetCode, card.Variant, card.Language}, "|")
		// A product's own Normal and Foil entries share its id and so are
		// one card twice; anything else wearing the identity is a second
		// card. A hand-carried printing sells as no product, so it stands
		// for itself under its own uuid - keying those on the absent
		// product id would make every one of them the same card and wave
		// through exactly the collision this catches.
		bearer := fmt.Sprintf("product %d", card.ExternalLinks.TcgPlayerID)
		if card.ExternalLinks.TcgPlayerID == 0 {
			bearer = "card " + card.ID
		}
		if other, seen := identities[identity]; seen && other != bearer {
			return out, fmt.Errorf("%s and %s wear one identity: %s",
				other, bearer, identity)
		}
		identities[identity] = bearer
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		// Only products are counted against the catalog's skus. A
		// hand-carried printing answers to no product and would otherwise
		// pile every one of its finishes under product 0, which the
		// coverage check would then have to explain.
		if productID := card.ExternalLinks.TcgPlayerID; productID != 0 {
			if sliceContains(gotFinishes[productID], card.Finish) {
				return out, fmt.Errorf("product %d carries finish %q twice", productID, card.Finish)
			}
			gotFinishes[productID] = append(gotFinishes[productID], card.Finish)
		}
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
