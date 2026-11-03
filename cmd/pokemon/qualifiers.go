package main

import (
	"regexp"
	"strings"

	"github.com/mtgban/datastore-gen/internal/handtable"
	"github.com/mtgban/go-tcgplayer"
)

var parenTailRe = regexp.MustCompile(`\s*\(([^)]*)\)$`)
var bracketTailRe = regexp.MustCompile(`\s*\[([^\]]*)\]$`)

// qual is one name qualifier with the delimiter style it wore, kept so an
// elected name part is restored in its own brackets.
type qual struct {
	text    string
	bracket bool
}

func (q qual) String() string {
	if q.bracket {
		return "[" + q.text + "]"
	}
	return "(" + q.text + ")"
}

// single is one card product, its name split into the base name, the
// qualifiers still up for election, and the ones dropped for restating the
// number or the rarity — kept around so the collision guard can take a drop
// back.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []qual
	dropped  []qual
}

// foldQualKey is what two spellings of one qualifier have in common: no
// case, no punctuation, and no plural on the end. One "s" is dropped rather
// than every trailing one, and only from a word long enough to still be a
// word without it, so "boss" does not become "bos".
// Case is folded away with everything else except on "ex", where it is the
// whole meaning: "Charizard ex" is a Scarlet & Violet card and "Charizard
// EX" an XY one, two mechanics a decade apart that a case-blind key would
// read as one spelling of the other.
func foldQualKey(qual string) string {
	var key strings.Builder
	for _, field := range strings.Fields(qual) {
		if strings.EqualFold(field, "ex") {
			key.WriteString(field)
			continue
		}
		key.WriteString(mtgmatcherNormalize(field))
	}
	out := foldPunctuationOnly(key.String())
	if out == "" {
		out = strings.ToLower(strings.TrimSpace(qual))
	}
	if len(out) > 3 && strings.HasSuffix(out, "s") {
		out = out[:len(out)-1]
	}
	return out
}

// qualNameKey identifies one card's name paired with one qualifier, folded
// so the catalog's punctuation and casing never decide.
func qualNameKey(base, qual string) string {
	folded := mtgmatcherNormalize(qual)
	if folded == "" {
		folded = strings.ToLower(strings.TrimSpace(qual))
	}
	return mtgmatcherNormalize(base) + "|" + folded
}

// foldPunctuationOnly keeps a qualifier that is nothing but punctuation
// telling itself apart. Unseen Forces names two Unown "!" and "?", and a
// key that drops every non-alphanumeric makes those the same qualifier and
// spells both of them "?".
func foldPunctuationOnly(qual string) string {
	return mtgmatcherNormalize(qual)
}

// dexNameQuals collects the parentheticals tcgdex prints as part of a card's
// own name. Thirteen of its 23,546 names carry one and they are all the same
// thing, the character a Supporter names: "Boss's Orders (Giovanni)",
// "Professor's Research (Professor Oak)", "Thought Wave Machine (Rocket's
// Secret Machine)". Everything else the catalog hangs off a name belongs to
// the printing.
//
// This used to be an election: a qualifier every product of a (group,
// number) bucket carried was taken to be part of the card, on the reasoning
// that what tells none of them apart cannot be telling them apart. The
// reasoning does not survive contact with upstream. Of the 549 entries whose
// names the election had spelled with a parenthetical, tcgdex carries the
// parenthetical on none, the bare name on 500, and a name differing only in
// how an EX is hyphenated on the other 49 - so every name it ever made was
// a name no card prints. "Prerelease" was 302 of them and "Team Plasma" 196,
// neither of which appears in any of tcgdex's 23,546 names.
//
// Upstream is asked rather than a vocabulary kept because a vocabulary has
// to be guessed at, and which parentheticals belong to a name is a fact
// somebody already recorded. tcgdex's own spelling is read raw here: putting
// it through catalogSpelling would turn every Delta Species mark into a
// parenthetical and teach this the opposite of what it is for.
// The value is how upstream writes the qualifier, not how the catalog does.
// Both spell the same thing differently - the catalog brackets the Unown
// letter, "Unown [A]" and "Unown (J)", where tcgdex writes "Unown A" - and
// keeping the catalog's delimiter left one card family spelled three ways.
func dexNameQuals(cards []tcgdexCard) map[string]string {
	names := map[string]bool{}
	for i := range cards {
		names[mtgmatcherNormalize(cards[i].Name)] = true
	}
	out := map[string]string{}
	for i := range cards {
		name := cards[i].Name
		if m := parenTailRe.FindStringSubmatch(name); m != nil {
			if base := strings.TrimSpace(strings.TrimSuffix(name, m[0])); base != "" {
				qual := strings.TrimSpace(m[1])
				out[qualNameKey(base, qual)] = "(" + qual + ")"
			}
			continue
		}
		// Upstream brackets some of them: the Unown letters it does not
		// write bare, and "Ancient Technical Machine [Rock]" beside its Ice
		// and Steel. Thirty names carry one, and reading only the
		// parenthesised ones filed "[Rock]" as a promo type on a card whose
		// two siblings differ from it by nothing else.
		if m := bracketTailRe.FindStringSubmatch(name); m != nil {
			if base := strings.TrimSpace(strings.TrimSuffix(name, m[0])); base != "" {
				qual := strings.TrimSpace(m[1])
				out[qualNameKey(base, qual)] = "[" + qual + "]"
			}
			continue
		}
		// Upstream also writes a name qualifier with nothing around it,
		// which is how the Unown letters are spelled: "Unown A", "Unown !",
		// "Unown ?". The catalog parenthesises them - "Unown (!)" - and
		// they would otherwise leave as promo types called "!" and "?".
		//
		// The last word is only taken as a qualifier when what precedes it
		// is a card name in its own right, which is what keeps "Dark
		// Tyranitar" from being read as a Tyranitar qualified by "Dark".
		// Read with the marks spelled the way the catalog spells them, so
		// "Nidoran \u2640" is indexed as the "Nidoran (F)" the catalog
		// writes and the gender stays part of the name instead of leaving
		// as a promo type called "f". Only the marks: catalogSpelling would
		// also turn every Delta Species into a parenthetical and teach this
		// the opposite of what it is for.
		name = strings.Join(strings.Fields(tcgdexSymbols.Replace(name)), " ")
		idx := strings.LastIndex(name, " ")
		if idx <= 0 {
			continue
		}
		base, tail := name[:idx], strings.TrimSpace(name[idx+1:])
		if tail == "" || !names[mtgmatcherNormalize(base)] {
			continue
		}
		out[qualNameKey(base, tail)] = tail
	}
	return out
}

// cosmosSpelling matches the holo pattern the catalog names five ways.
var cosmosSpelling = regexp.MustCompile(`(?i)\bCosmos?[ ]+(?:Holofoil|Holo|Foil)\b`)

// oneCosmos spells that pattern the one way. The catalog files it as
// "Cosmos Holo" 170 times, "Cosmo Holo" 17, "Cosmos Foil" 7, "Cosmo Foil"
// 6 and "Cosmos Holofoil" 4: one printing wearing five labels, so a query
// naming any of them misses the cards filed under the other four.
//
// Only the pattern's own words are rewritten. The qualifiers around it are
// what tell real printings apart - a "Reverse Cosmos Holo" and a "Cosmos
// Holo Costco Exclusive" are not the plain one - and they are left as they
// are. No card carries two of the five spellings at one identity, so this
// folds no two printings into one. The Pokemon named Cosmog is not
// matched: the pattern needs a treatment word after the stem.
func oneCosmos(qualifier string) string {
	return cosmosSpelling.ReplaceAllString(qualifier, "Cosmos Holo")
}

// gameStopSpelling matches the retailer the catalog names eight ways over
// twenty-four products: "Gamestop Exclusive" 12 times, "GameStop Exclusive"
// 4, "Gamestop Promo" 3, and one each of "GameStop Promo", "GameStop Metal
// Card", "Gamestop" inside "Non-Holo Gamestop Exclusive", "GameStop" and
// "Game Stop".
var gameStopSpelling = regexp.MustCompile(`(?i)\bGame\s*Stop\b`)

// oneGameStop writes the retailer's name the way the retailer writes it.
// Frequency says "Gamestop" and the election below would duly elect it, but
// a majority of a typo is still a typo. Rewriting the word rather than the
// labels is what keeps the family together: the bare label, the two it
// leads, and the one it sits in the middle of all read alike, and a label
// the catalog has not invented yet will too.
func oneGameStop(qualifier string) string {
	return gameStopSpelling.ReplaceAllString(qualifier, "GameStop")
}

// trimQualJoiners takes off a character the catalog uses to join two things
// and left dangling at the end of one. It closes two Ralts numbers with a
// stray slash, "(060/189/)" and "(067/195/)", which is enough to stop the
// number being read as a number: the qualifier survived the drop, the split
// took "060/189" off the front, and what it left behind became a promo type
// called "/". No qualifier the catalog means ends in one of these.
func trimQualJoiners(qualifier string) string {
	return strings.TrimRight(strings.TrimSpace(qualifier), " /,-")
}

// spelledNumbers writes a placing the way the rest of them are written.
// The catalog spells two of them out - "2014 Top Sixteen" beside "Top 16",
// "Top Thirty-Two" beside "Top 32" - and a word and a numeral fold to no
// common key however they are normalised.
var spelledNumbers = strings.NewReplacer(
	"Thirty-Two", "32", "Thirty Two", "32", "Sixteen", "16",
)

// blackStarRe matches the promo shelf the catalog names singular and
// plural in one breath: "SM Black Star Promo" 21 times beside "XY Black
// Star Promos" 11, and each spelling elected its own because the election
// compares one label at a time. The shelf is "Black Star Promos".
var blackStarRe = regexp.MustCompile(`(?i)black star promos?$`)

func oneBlackStar(qualifier string) string {
	return blackStarRe.ReplaceAllString(qualifier, "Black Star Promos")
}

// respellQual writes a qualifier the one way this datastore spells it,
// where the catalog spells it several.
func respellQual(qualifier string) string {
	q := trimQualJoiners(qualifier)
	return oneBlackStar(oneWorldsLabel(spelledNumbers.Replace(oneGameStop(oneCosmos(q)))))
}

// peelQuals peels the trailing parenthetical and bracket qualifiers off a
// name, outermost last, preserving their order and dropping a repeat of a
// qualifier the product already carries.
func peelQuals(name string) (string, []qual) {
	var quals []qual
	for {
		var q qual
		if m := parenTailRe.FindStringSubmatch(name); m != nil {
			q = qual{text: respellQual(strings.TrimSpace(m[1]))}
			name = strings.TrimSuffix(name, m[0])
		} else if m := bracketTailRe.FindStringSubmatch(name); m != nil {
			q = qual{text: respellQual(strings.TrimSpace(m[1])), bracket: true}
			name = strings.TrimSuffix(name, m[0])
		} else {
			break
		}
		if q.text == "" {
			continue
		}
		duplicate := false
		for _, seen := range quals {
			if seen.text == q.text {
				duplicate = true
			}
		}
		if !duplicate {
			quals = append([]qual{q}, quals...)
		}
	}
	return strings.TrimSpace(name), quals
}

// droppedQuals name a label that survives the peel and says nothing worth
// carrying. "English" is the language every card in this datastore is
// printed in, so it distinguishes a Pikachu from nothing; the six other
// languages beside it in the same set do distinguish theirs and stay.
var droppedQuals = handtable.New("droppedQuals", map[string]bool{
	"english": true,
})

// setOfRe matches the count a multi-card product carries in its name,
// written either way: "Zacian V-UNION [Set of 4]" and "Code Card - Mini
// Portfolio [Giratina] (2-Pack)".
var setOfRe = regexp.MustCompile(`(?i)^(?:set of \d+|\d+-pack)$`)

// isBareLetter reports whether a qualifier is a single letter and nothing
// else. Where the letter belongs to the card, upstream says so and it has
// already been taken into the name - the Unown letters, Nidoran's gender.
// What is left is a letter the catalog wrote and the number already says:
// "M Charizard EX (X)" is numbered 69 where its Y is numbered 13, and a
// promo type spelled "x" helps no one read that.
func isBareLetter(qualifier string) bool {
	if len([]rune(qualifier)) != 1 {
		return false
	}
	r := []rune(strings.ToLower(qualifier))[0]
	return r >= 'a' && r <= 'z'
}
