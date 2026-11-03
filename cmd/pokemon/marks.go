package main

import (
	"fmt"
	"regexp"
	"strings"
)

// datePrintings gives a printing the date its labels stated, where its set
// does not already state it. See promoReleaseDate.
func datePrintings(cards []any, sets map[string]any, stated map[string]string) int {
	var dated int
	for _, raw := range cards {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		year := stated[fmt.Sprint(item["id"])]
		if year == "" {
			continue
		}
		setDate := ""
		if held, ok := sets[fmt.Sprint(item["setCode"])].(map[string]any); ok {
			setDate = fmt.Sprint(held["releaseDate"])
		}
		if date := promoReleaseDate(year, setDate); date != "" {
			item["originalReleaseDate"] = date
			dated++
		}
	}
	return dated
}

// markPrintings gives a printing the mark saying which copy of its collector
// number it is, where nothing else says. The labels it marks with are the
// ones promoTypesOf left out - a Pokemon it pictures, a set it was reprinted
// from, the deck code CLB, CLC or CLV - each of which is rightly no
// promotion, and any of which may still be the whole of what tells two
// printings apart.
//
// "Energy Search" is card 9 of two Latias and Latios theme decks, one number
// and one rarity between them; "Sceptile" 010 was reprinted from EX Emerald
// and from DP Stormfront. The builder's own guard already keeps these apart
// in the joined variant, so nothing was lost - but a consumer reading the
// fields rather than the sentence had no way to tell them apart, and 582
// printings read as duplicates of each other.
func markPrintings(cards []any, leftOut map[string][]string) (int, int) {
	identity := func(item map[string]any) string {
		return fmt.Sprint(item["name"], "|", item["number"], "|", item["total"], "|", item["setCode"], "|",
			item["rarity"], "|", item["finish"], "|", item["promoTypes"], "|", item["watermark"], "|",
			item["originalReleaseDate"], "|", item["language"])
	}
	shared := map[string]int{}
	for _, raw := range cards {
		if item, ok := raw.(map[string]any); ok {
			shared[identity(item)]++
		}
	}
	var marked int
	for _, raw := range cards {
		item, ok := raw.(map[string]any)
		if !ok || shared[identity(item)] < 2 {
			continue
		}
		left := leftOut[fmt.Sprint(item["id"])]
		if len(left) == 0 {
			continue
		}
		item["watermark"] = strings.Join(left, " ")
		marked++
	}
	left := map[string]int{}
	for _, raw := range cards {
		if item, ok := raw.(map[string]any); ok {
			left[identity(item)]++
		}
	}
	var alike int
	for _, n := range left {
		if n > 1 {
			alike += n - 1
		}
	}
	return marked, alike
}

// runSeries splits the instalment off a promotion that runs in numbered
// ones. "Prize Pack Series 1" and its Series 2 are one promotion run twice,
// and which running a card came from is a mark rather than a promotion -
// the same shape as a printing's version.
//
// A season is an instalment written as two years: "Asia Championship Series
// 23-24" and its 24-25 are that promotion run twice, and the two digits are
// too few for the year rule to see.
var runSeries = regexp.MustCompile(`(?i)^(.*\bseries)\s+([0-9]{1,2}\s*[-/]\s*[0-9]{1,2}|[0-9]{1,2})$`)

// runYear is a year a qualifier states, and runSpan the two a season is
// written with. A season is not a date - "2004-2005" is a year and the next
// one, and no single day stands for both - so a label naming one keeps it.
var runYear = regexp.MustCompile(`\b(19|20)[0-9]{2}\b`)
var runSpan = regexp.MustCompile(`\b(?:19|20)[0-9]{2}\s*-\s*(?:19|20)[0-9]{2}\b`)

// promoYear takes the year off a label and hands back what the label says
// besides. When a promotion ran is not what it is: "World Championships
// 2005" and its 2013 are one promotion run twice, and eighteen of these
// labels were the same promotion written with eighteen different years.
//
// A label that is a year and nothing else says only when, so nothing is
// left of it and the date carries the whole of what it said.
func promoYear(label string) (rest, year string) {
	if runSpan.MatchString(label) {
		return label, ""
	}
	match := runYear.FindString(label)
	if match == "" {
		return label, ""
	}
	return strings.Join(strings.Fields(runYear.ReplaceAllString(label, " ")), " "), match
}

// promoReleaseDate is the date a year states, published only where the set
// does not already state it. The promo shelves are why it is worth stating:
// "League & Championship Cards" is one set holding cards handed out from
// 2006 to 2011, and the shelf is dated 2016. 262 of the 344 printings whose
// label names a year name one their set disagrees with.
//
// A year names no day, so the first of January stands in - as much as the
// label knows. Where the set's year agrees, the set's own day is better and
// nothing is published.
func promoReleaseDate(year, setDate string) string {
	if year == "" || len(setDate) >= 4 && setDate[:4] == year {
		return ""
	}
	return year + "-01-01"
}

// printMark is the mark saying which copy of a collector number a printing
// is, rather than what promoted it. A World Championship card is one of a
// player's deck, and the same card at the same number is in several
// players' decks: the name is what tells those copies apart and nothing
// promoted any of them, so it is published as a mark of its own rather than
// counted among the promotions.
func printMark(s *single, setCode string) string {
	if !strings.HasPrefix(setCode, worldsSetStem) {
		return ""
	}
	match := wcdPlayerRe.FindStringSubmatch(s.product.Name)
	if match == nil {
		return ""
	}
	// Spelled the one way this datastore spells it. The catalog writes
	// "Jeremy Moran" where the player is Jeremy Maron, and the qualifier
	// beside this is already corrected - so a mark taken raw would be the
	// same name twice, once each way, and the qualifier would go on
	// reading as a promotion for failing to match.
	named := strings.ToLower(respellQual(strings.TrimSpace(match[1])))
	if fixed, hand := qualSpellings.Get(named); hand {
		named = strings.ToLower(fixed)
	}
	// And without whatever is written beside it. The 2024 decks put the
	// player and the treatment in one parenthetical - "(Jesse Parker Gold
	// Signature)" - where every other year keeps them apart, so the
	// qualifier the name opens with is the player and the rest of it is
	// the promotion it was sold with.
	for _, q := range s.quals {
		text := strings.ToLower(q.text)
		if text != named && strings.HasPrefix(named, text) {
			return text
		}
	}
	return named
}
