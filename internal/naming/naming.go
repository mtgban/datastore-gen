// Package naming reads the qualifiers a TCGplayer product name carries, for
// the builders that elect a card's name from its products by collector
// number with no upstream witness to say which parenthetical is the name:
// Gundam's tokens and Palworld. It splits a name into its base and its
// qualifiers, and says which qualifiers are only an echo of another field or
// say where a printing came from rather than which card it is.
package naming

import (
	"regexp"
	"strings"
)

var parenRe = regexp.MustCompile(`\s*\(([^)]+)\)`)

// Split splits a product name into the base name and its parentheticals,
// dropping the collector number worn as decoration.
func Split(name, number string) (base string, qualifiers []string) {
	if number != "" {
		name = strings.ReplaceAll(name, " - "+number, "")
	}
	name = parenRe.ReplaceAllStringFunc(name, func(m string) string {
		q := strings.TrimSpace(strings.Trim(strings.TrimSpace(m), "()"))
		if q == "" || strings.EqualFold(q, number) {
			return ""
		}
		qualifiers = append(qualifiers, q)
		return ""
	})
	return strings.Join(strings.Fields(name), " "), qualifiers
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

// Redundant reports whether a qualifier says only what another field on the
// entry already says. Two fields say it: the rarity, which a name restates
// outright ("(C+)" on a C+ printing), spells with "Rare" elided, or
// shorthands to its initials; and the collector number, whose own suffix
// some of these games repeat in the name ("...-001TSR" beside "(TSR)").
// Either way the entry keeps the field and drops the echo, so a query for
// the card's name is not asked to carry the rarity too.
func Redundant(qualifier, rarity, number string) bool {
	q := strings.ToUpper(nonAlnumRe.ReplaceAllString(qualifier, ""))
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

var wordRe = regexp.MustCompile(`[a-z0-9']+`)

// Provenance reports whether a qualifier says where a printing came from
// rather than which card it is. Such a qualifier may never be elected into
// a name, however many printings of a number carry it.
//
// The election reads a qualifier every printing of a number carries as part
// of the card's name, which is right for a Gundam mobile suit's form and its
// faction - "(MA Mode)", "(Sleeves)" - and wrong for a promo whose only
// printings came out of one box: "Resource (RP-045) (EVX07 Resource Set)" is
// the same Resource card the set sells, and a name carrying that is a name
// no storefront writes and no search for the card finds. Every Palworld
// number holds a single product, so that is the shape the election would
// meet there every time it fired. A word list rather than a table of
// spellings, because the spellings are open-ended - every season brings
// another judge pack or promotional box - while the words they are built
// from are not. Whole words only: a card named "(Full Package)" is not a
// pack.
func Provenance(qualifier string) bool {
	for _, word := range wordRe.FindAllString(strings.ToLower(qualifier), -1) {
		if provenanceWords[word] {
			return true
		}
	}
	return false
}
