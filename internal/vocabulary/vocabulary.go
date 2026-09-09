// Package vocabulary checks that a published datastore's promo types read
// the way every builder here agrees they should.
//
// The rules are the ones the builders follow, stated once so that a game
// cannot drift from them quietly. Two bugs reached master without a check
// like this: a builder published a shelf's whole product name as one token,
// and another put back as tokens the subjects and numbering it had just
// taken out.
package vocabulary

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// TokenLimit is how long a promo type may read before it stops being a name
// and starts being a sentence. A query carries a token, and a token nobody
// can type is one nobody will.
const TokenLimit = 22

// slugRe is everything a token is: a promo type reaches a query as one
// word, because a search splits its words apart before a filter sees them.
var slugRe = regexp.MustCompile(`^[a-z0-9]+$`)

// Printing is the part of a published card these checks read: one entry per
// priced finish, with the facts that tell it from its siblings.
type Printing struct {
	ID         string
	Name       string
	Number     string
	SetCode    string
	Rarity     string
	Finish     string
	PromoTypes []string
	Watermark  string
	Date       string
	Language   string
}

// Slug is a label as the token a query can carry.
func Slug(label string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Problems are every way a datastore's vocabulary departs from the rules,
// each named with enough of the printing to find it.
type Problems struct {
	// NotSlugs are tokens carrying something a query cannot: a space, a
	// dot, a capital. The builder publishes the token and the loader keeps
	// the words.
	NotSlugs []string

	// TooLong are tokens past TokenLimit, which are a shelf's product name
	// rather than a promotion's.
	TooLong []string

	// RarityEchoes are tokens saying what the rarity field says. A rarity
	// is not a label: as a tag it declares a printing promotional for
	// being the rarity it is.
	RarityEchoes []string

	// FinishEchoes are tokens saying what the finish field says.
	FinishEchoes []string

	// Alike are the printings no published field tells apart, each group
	// listed by id. Every fold has to leave the printings it folded still
	// distinguishable, and a fact that does the telling belongs in a field
	// rather than in prose.
	Alike [][]string
}

// Any reports whether anything was found.
func (p Problems) Any() bool {
	return len(p.NotSlugs)+len(p.TooLong)+len(p.RarityEchoes)+len(p.FinishEchoes)+len(p.Alike) > 0
}

// Lines are the problems as one line each, for a log or a test failure.
func (p Problems) Lines() []string {
	var out []string
	say := func(what string, found []string) {
		if len(found) == 0 {
			return
		}
		shown := found
		if len(shown) > 8 {
			shown = shown[:8]
		}
		out = append(out, fmt.Sprintf("%d %s: %s", len(found), what, strings.Join(shown, ", ")))
	}
	say("tokens are not slugs", p.NotSlugs)
	say(fmt.Sprintf("tokens read past %d characters", TokenLimit), p.TooLong)
	say("tokens say what the rarity field says", p.RarityEchoes)
	say("tokens say what the finish field says", p.FinishEchoes)
	if len(p.Alike) > 0 {
		var printings int
		var shown []string
		for _, group := range p.Alike {
			printings += len(group)
			if len(shown) < 3 {
				shown = append(shown, strings.Join(group, " = "))
			}
		}
		out = append(out, fmt.Sprintf("%d printings in %d groups no published field tells apart: %s",
			printings, len(p.Alike), strings.Join(shown, "; ")))
	}
	return out
}

// Check reads a datastore's printings against the rules.
func Check(printings []Printing) Problems {
	var found Problems
	seen := map[string]bool{}
	alike := map[string][]string{}
	var order []string
	for _, printing := range printings {
		for _, token := range printing.PromoTypes {
			switch {
			case !slugRe.MatchString(token):
				if !seen["slug:"+token] {
					seen["slug:"+token] = true
					found.NotSlugs = append(found.NotSlugs, token)
				}
			case len(token) > TokenLimit:
				if !seen["long:"+token] {
					seen["long:"+token] = true
					found.TooLong = append(found.TooLong, token)
				}
			}
			if rarity := Slug(printing.Rarity); rarity != "" && token == rarity && !seen["rarity:"+token] {
				seen["rarity:"+token] = true
				found.RarityEchoes = append(found.RarityEchoes, token)
			}
			if finish := Slug(printing.Finish); finish != "" && token == finish && !seen["finish:"+token] {
				seen["finish:"+token] = true
				found.FinishEchoes = append(found.FinishEchoes, token)
			}
		}
		identity := strings.Join([]string{
			printing.Name, printing.Number, printing.SetCode, printing.Rarity, printing.Finish,
			strings.Join(printing.PromoTypes, "+"), printing.Watermark, printing.Date, printing.Language,
		}, "|")
		if _, held := alike[identity]; !held {
			order = append(order, identity)
		}
		alike[identity] = append(alike[identity], printing.ID)
	}
	for _, identity := range order {
		if len(alike[identity]) > 1 {
			found.Alike = append(found.Alike, alike[identity])
		}
	}
	sort.Strings(found.NotSlugs)
	sort.Strings(found.TooLong)
	sort.Strings(found.RarityEchoes)
	sort.Strings(found.FinishEchoes)
	return found
}
