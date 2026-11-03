package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/mtgban/go-tcgplayer"
)

// numberLikeRe matches the shapes collector numbers take in this catalog —
// "8/102", "SWSH001", "TG17/TG30", "104a/102" — so a dash tail that looks
// like a number but disagrees with the Number field can be warned about.
// Bare digit runs longer than three are years ("Torchic - 2004"), not
// numbers.
var numberLikeRe = regexp.MustCompile(`^(?:[A-Za-z]{1,6}\d{1,4}[a-z]?|\d{1,3}[a-z]?)(?:[/-](?:[A-Za-z]{1,6}\d{1,4}[a-z]?|\d{1,3}[a-z]?))?$`)

// numberLedRe splits a qualifier that opens with the card's own collector
// number from the label behind it: "(147 Full Art)", "(#13 - Non-Holo)",
// "(8 Delta)". The number is dropped the way a qualifier that is nothing
// but the number already was, and what is left joins the label it is one
// of - 202 products carried one, and their labels are Full Art 116 times,
// Secret Rare 15, Non-Holo 14, Holo 12.
//
// Left whole they are 202 labels no query will ever name, and two of them
// looked like a spelling to fold: "(#13 - Non-Holo)" on a Noivern numbered
// 13 and "(#13 Non-Holo)" on a Latios numbered 13 are not two spellings of
// one label, they are two cards each restating their own number.
// The separator is whatever the catalog put between them: a space, a dash,
// or a comma - "Great Ball (#21, Alolan Sandslash Half-Deck)" - and all of
// it goes, or the label keeps a leading comma and stops being the label its
// siblings carry.
var numberLedRe = regexp.MustCompile(`^#?([0-9]+[a-zA-Z]?(?:/[0-9]+)?)[\s,-]*([^\s,-].*)$`)

// numberJoinRe splits a qualifier that hangs the card's own number between
// two labels rather than in front of one: "(Alpha - 149 Full Art)" on a
// Primal Kyogre EX numbered 149 is two labels, Alpha and Full Art, with a
// number in the middle that says nothing the number field does not.
//
// The dash is what says so. "Best of Game 6 Promo" on a card numbered 6
// carries its number the same way and is left whole, because there is no
// dash to read it as a join and the phrase is the promo's own name - four
// products, against these two.
var numberJoinRe = regexp.MustCompile(`^(\S.*?)\s+-\s+#?([0-9]+[a-zA-Z]?(?:/[0-9]+)?)\s+([^\s,-].*)$`)

// unnumberedRe matches the word and nothing else around it.
var unnumberedRe = regexp.MustCompile(`(?i)\bunnumbered\b`)

// bracketInnerRe matches a qualifier that closes with a bracketed one of
// its own, which is how the catalog writes two labels in one parenthesis:
// "Vivillon (High Plains [Orange])" is the pattern and the colour, and
// "Vivillon (Meadow [Pink])" beside it is another of each. Peeling reads
// the outer delimiter and stops, so both arrived as a single label no other
// card shares.
var bracketInnerRe = regexp.MustCompile(`^(\S.*?)\s*\[([^\[\]]+)\]$`)

// decompose splits a product name into base name and qualifiers, strips the
// dash-hung collector number, and applies the pre-election drops. The dash
// number sits between the name and the trailing qualifiers, so the
// qualifiers peel first and the tail check runs on what remains.
func decompose(p tcgplayer.Product, num, year string) (single, int) {
	name := p.Name
	if repaired, hand := rawNames.Get(p.ProductID); hand {
		name = repaired
	}
	if year != "" {
		name = strings.TrimSpace(worldsYearTailRe.ReplaceAllString(name, "$2"))
	}
	base, quals := peelQuals(name)

	// A name that hangs its qualifier off a dash of its own leaves that
	// dash behind once the qualifier is peeled, and the dash number then
	// looks like it ends the name rather than sitting before it.
	for {
		base = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(base), "-"))

		idx, skip := strings.LastIndex(base, " - "), 3
		if idx < 0 {
			// The catalog also writes the number with no dash at all,
			// "Gyarados 21/98" and, once the qualifiers are off,
			// "Multi Energy (Special) 93/100". Only a tail that restates
			// the Number field is taken, so a name whose last word is a
			// number of its own keeps it.
			idx, skip = strings.LastIndex(base, " "), 1
		}
		if idx < 0 {
			break
		}
		tail := strings.TrimSpace(base[idx+skip:])
		if skip == 1 {
			// The dash written without its space, "Ninetales -199/197"
			// on six products.
			tail = strings.TrimPrefix(tail, "-")
		}
		if !restatesNumber(tail, num) {
			// A number-shaped tail that disagrees with the Number field is
			// a number all the same - "Exploud - 3/106" filed at 003/109,
			// "Jirachi V - 299" at SWSH299 - and no card is named with one.
			// It comes off the name and is said out loud.
			if skip == 3 && numberLikeRe.MatchString(tail) {
				log.Printf("dash number: %q drops tail %q, Number is %q", p.Name, tail, num)
				base = strings.TrimSpace(base[:idx])
				continue
			}
			break
		}
		base = strings.TrimSpace(base[:idx])

		// The catalog hangs the number on either side of the qualifiers.
		// "Oricorio - 55/145 (League Challenge)" writes it in front of them
		// and "Darkrai (Team Plasma) - BW73" behind, and a qualifier written
		// the second way is not at the tail until the number is gone - so
		// peeling once and stripping once left 117 names still wearing one.
		peeled, more := peelQuals(base)
		if len(more) == 0 {
			break
		}
		base = peeled
		for _, q := range more {
			duplicate := false
			for _, seen := range quals {
				if seen.text == q.text {
					duplicate = true
				}
			}
			if !duplicate {
				quals = append(quals, q)
			}
		}
	}

	// A qualifier holding a bracketed one becomes the two it holds, before
	// anything below reads either: each half is a label in its own right
	// and each has to face the number and rarity checks on its own.
	var expanded []qual
	for _, q := range quals {
		if m := bracketInnerRe.FindStringSubmatch(q.text); m != nil {
			expanded = append(expanded,
				qual{text: strings.TrimSpace(m[1]), bracket: q.bracket},
				qual{text: strings.TrimSpace(m[2]), bracket: true})
			continue
		}
		expanded = append(expanded, q)
	}
	quals = expanded

	// "Unnumbered" on a card that carries no number says what the empty
	// Number field says. Ten labels wore it - "2005 Unnumbered", "2007
	// Unnumbered D/P Style Non-Holo" - and the word is the only thing
	// keeping them from the years and treatments they otherwise name.
	// Dropped only where the card really is unnumbered, so a label saying
	// it of a numbered card stays and can be read as the mistake it is.
	if num == "" {
		for i := range quals {
			without := unnumberedRe.ReplaceAllString(quals[i].text, " ")
			quals[i].text = strings.Join(strings.Fields(without), " ")
		}
	}

	rarity := p.Extended("Rarity")
	s := single{product: p, number: num, baseName: base}
	var numberLed int
	for _, q := range quals {
		if m := numberJoinRe.FindStringSubmatch(q.text); m != nil && restatesNumber(m[2], num) {
			s.quals = append(s.quals, qual{text: strings.TrimSpace(m[1]), bracket: q.bracket})
			q.text = strings.TrimSpace(m[3])
			numberLed++
		}
		if m := numberLedRe.FindStringSubmatch(q.text); m != nil && restatesNumber(m[1], num) {
			q.text = strings.TrimSpace(m[2])
			numberLed++
		}
		// A number the label carries in the middle of itself, which says
		// what the Number field says: "Best of Game 6 Promo" on the card
		// numbered 6, and its 1, 2 and 7. Only the middle - a number at
		// either end is the label's own, "Top 16" on a card numbered 16 and
		// "#47 Charizard Stamped" on the 47th card of a deck.
		if fields := strings.Fields(q.text); len(fields) > 2 {
			kept := make([]string, 0, len(fields))
			for i, field := range fields {
				if i > 0 && i < len(fields)-1 && restatesNumber(strings.TrimPrefix(field, "#"), num) {
					numberLed++
					continue
				}
				kept = append(kept, field)
			}
			if len(kept) != len(fields) {
				q.text = strings.Join(kept, " ")
			}
		}
		if restatesNumber(strings.TrimPrefix(q.text, "#"), num) || restatesRarity(q.text, rarity) {
			s.dropped = append(s.dropped, q)
			continue
		}
		s.quals = append(s.quals, q)
	}

	// The hand splits read last, on the label the number has come off:
	// "Mewtwo EX (163 Secret Full Art)" is not "Secret Full Art" until the
	// 163 restating its own number is gone.
	var split []qual
	for _, q := range s.quals {
		parts, hand := qualSplits.Get(strings.ToLower(q.text))
		if !hand {
			split = append(split, q)
			continue
		}
		for _, part := range parts {
			split = append(split, qual{text: part, bracket: q.bracket})
		}
	}
	s.quals = split
	return s, numberLed
}

// electionKey identifies the (group, number) bucket a name-versus-variant
// call is made in.
func electionKey(s *single) string {
	return fmt.Sprintf("%d|%s", s.product.GroupID, s.number)
}
