package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// sanitizeID reduces a string to the id alphabet: lowercase alphanumerics
// with runs of anything else collapsed to single dashes.
func sanitizeID(s string) string {
	mapped := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, strings.ToLower(s))
	for strings.Contains(mapped, "--") {
		mapped = strings.ReplaceAll(mapped, "--", "-")
	}
	return strings.Trim(mapped, "-")
}

// emitNumber writes a collector number onto an entry with the card's part
// and the set total apart: the "082/167" a card face prints is the card's
// own "082" plus the set's size, and the size is the set's fact rather
// than part of the card's identity. The cut is at the first slash, which
// keeps a lettered subset's own part whole ("TG01/TG30" is card TG01) and
// leaves a face without a total untouched. Nothing is checked against the
// set's real size on purpose: the secret rares are numbered past it
// ("168/167") and that is what the face says.
func emitNumber(entry map[string]any, number string) {
	own, total, _ := strings.Cut(number, "/")
	entry["number"] = own
	if total != "" {
		entry["total"] = total
	}
}

// totalsBySet is the set total each set's cards agree on, for the sets that
// agree on one. A pooled set agrees on none - World Championship Decks and
// the promo pools hold cards that keep the total of wherever they were
// first printed, so 44/130 and 87/101 sit side by side - and those sets are
// absent rather than guessed at, because there is no one size to report.
func totalsBySet(cards []any) map[string]string {
	seen := map[string]map[string]bool{}
	numbers := map[string]map[string]bool{}
	for _, item := range cards {
		entry, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		code, _ := entry["setCode"].(string)
		total, ok := entry["total"].(string)
		if code == "" || !ok || total == "" {
			continue
		}
		if seen[code] == nil {
			seen[code] = map[string]bool{}
			numbers[code] = map[string]bool{}
		}
		seen[code][total] = true
		number, _ := entry["number"].(string)
		numbers[code][number] = true
	}
	out := map[string]string{}
	for code, totals := range seen {
		// One total, printed beside more than one number: a set left
		// holding one card - the World Championship shelf's one undated
		// product, the e-Reader samples - agrees with itself about nothing.
		if len(totals) != 1 || len(numbers[code]) < 2 {
			continue
		}
		for total := range totals {
			out[code] = total
		}
	}
	return out
}

// numberWidthsBySet is how wide the catalog pads a plain number in each set,
// for the sets that pad every one of them the same. A minted card takes its
// number from upstream, which pads nothing - tcgdex calls Base Set's Machamp
// card "8" where the catalog calls its 101 shelfmates "001" through "102" -
// and a number published narrower than the set it sits in is a number no
// search for the set's own spelling finds: the loader keeps what is
// published as Number and reduces it to PlainNumber separately, so "8" and
// "008" answer apart.
//
// Only a set that agrees with itself lends a width. The catalog pads by
// group rather than by category and 124 of its 223 numbered sets hold
// numbers of two or three widths at once; those say nothing about how wide a
// card of theirs should be and are left alone. Of the 358 minted entries, one
// sits in a set that agrees and is narrower than it: Machamp, the single Base
// Set card the catalog has no Base Set product for.
func numberWidthsBySet(cards []any) map[string]int {
	widths := map[string]map[int]bool{}
	for _, item := range cards {
		entry, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		code, _ := entry["setCode"].(string)
		number, _ := entry["number"].(string)
		if code == "" || number == "" || !isAllDigits(number) {
			continue
		}
		if widths[code] == nil {
			widths[code] = map[int]bool{}
		}
		widths[code][len(number)] = true
	}
	out := map[string]int{}
	for code, seen := range widths {
		if len(seen) != 1 {
			continue
		}
		for width := range seen {
			out[code] = width
		}
	}
	return out
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// mintedIDBase is the id stem of an entry that names no product: the
// upstream id reduced to the alphabet a uuid travels through. A catalog id
// always carries "_<product id>" before its finish suffix and a minted one
// never does, so the two namespaces cannot meet.
func mintedIDBase(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func idBase(num string, productID int) string {
	s := sanitizeID(num)
	if s == "" {
		return strconv.Itoa(productID)
	}
	return s + "_" + strconv.Itoa(productID)
}

// padRe matches the zeros a component pads its digits with, in front or
// behind a letter prefix.
var padRe = regexp.MustCompile(`^([A-Z]*)0+([0-9])`)

// unpad writes a component the one way, so "H01" and "H1" are one number
// and so are "045" and "45". The padding behind a letter is the one that
// was missed: the e-Card sets number their holos H01 to H32 and the catalog
// writes the qualifier unpadded, which left nine promo types spelled "h1"
// through "h9" saying what the number already said.
func unpad(s string) string {
	return padRe.ReplaceAllString(strings.ToUpper(strings.TrimSpace(s)), "$1$2")
}

// componentEq compares one side of a collector number case-insensitively
// and zero-padding-insensitively: the Number field says "004/102" where the
// name says "4/102".
func componentEq(a, b string) bool {
	return unpad(a) == unpad(b)
}

// restatesNumber reports whether a name token restates the Number field:
// numerators must agree, denominators too when both sides carry one —
// either side may carry only the numerator.
func restatesNumber(token, num string) bool {
	if num == "" {
		return false
	}
	tparts := strings.SplitN(token, "/", 2)
	nparts := strings.SplitN(num, "/", 2)
	if !componentEq(tparts[0], nparts[0]) {
		return false
	}
	if len(tparts) == 2 && len(nparts) == 2 {
		return componentEq(tparts[1], nparts[1])
	}
	return true
}

// saysFinish reports whether a tag says what the finish field already says
// of the printing. The catalog writes a printing's finish into its product
// name in more spellings than this datastore has finishes - "Holofoil",
// "Holo" and "Holo Common" for a Holofoil printing, "Non-Holo" for a Normal
// one - and the hand spellings fold the holo three onto "Holo", so a tag
// compared against the slug of the finish alone caught "holofoil" and left
// "holo" declaring 29 printings promotional and "nonholo" 74 for being the
// finish they are priced as. The wording stays in the variant either way.
func saysFinish(tag, finish string) bool {
	if tag == emit.PromoSlug(finish) {
		return true
	}
	switch tag {
	case "holo":
		return strings.HasSuffix(finish, "Holofoil") && !strings.HasPrefix(finish, "Reverse")
	case "nonholo":
		return finish == "Normal"
	case "reverseholo":
		return finish == "Reverse Holofoil"
	}
	return false
}

// restatesRarity reports whether a qualifier is a word-subset of the
// product's own Rarity ("(Secret)" under "Secret Rare").
func restatesRarity(qualifier, rarity string) bool {
	words := strings.Fields(strings.ToLower(rarity))
	if len(words) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, w := range words {
		set[w] = true
	}
	fields := strings.Fields(strings.ToLower(qualifier))
	if len(fields) == 0 {
		return false
	}
	for _, w := range fields {
		if !set[w] {
			return false
		}
	}
	return true
}

// numeratorKey normalizes one side of a collector number for the tcgdex
// join: uppercased, leading zeros stripped.
func numeratorKey(s string) string {
	key := strings.TrimLeft(strings.ToUpper(strings.TrimSpace(s)), "0")
	if key == "" {
		return "0"
	}
	return key
}
