package main

import (
	"regexp"
)

// wcdPlayerRe reads the player whose deck a World Championship card came
// in. The catalog writes the year and then the name, last of all: "Dark
// Tyranitar (19) - 2005 (Takashi Yoneda)", on 1,953 of the 1,968 singles
// the shelf sells.
var wcdPlayerRe = regexp.MustCompile(`-\s*[0-9]{4}\s*\(([^()]*)\)`)

// worldsSetStem opens the set codes the championship shelf is split into,
// WCD2004 through WCD2025.
const worldsSetStem = "WCD"

// worldsLabelRe write the championship the one way. The catalog names it
// four: "Worlds 07" through "Worlds 13", "2004 World Championships" with
// the year in front, "World Championship 2025" without its plural, and
// "World Championships 2017" which is what the other three become.
var (
	worldsShortRe    = regexp.MustCompile(`^Worlds\s+(\d{2})$`)
	worldsLeadYearRe = regexp.MustCompile(`^((?:19|20)\d{2})\s+World Championships?$`)
	worldsSingularRe = regexp.MustCompile(`^World Championship\s+((?:19|20)\d{2})$`)
)

func oneWorldsLabel(qualifier string) string {
	if m := worldsShortRe.FindStringSubmatch(qualifier); m != nil {
		return "World Championships 20" + m[1]
	}
	if m := worldsLeadYearRe.FindStringSubmatch(qualifier); m != nil {
		return "World Championships " + m[1]
	}
	if m := worldsSingularRe.FindStringSubmatch(qualifier); m != nil {
		return "World Championships " + m[1]
	}
	return qualifier
}

// worldsGroupRe matches the catalog group that is not one set. TCGplayer
// files every World Championship deck ever printed on one shelf, twenty
// championships from 2004 to 2025, and the year is the only thing telling
// two printings of a card apart: 226 (name, number, finish) keys there hold
// two or more entries separated by nothing else. It is also why the group
// can publish no set size - its cards keep the total of wherever they were
// first printed, so 44/130 sits beside 87/101 - and a set per year agrees
// on one.
var worldsGroupRe = regexp.MustCompile(`(?i)world championship deck`)

// worldsYearTailRe reads the year off a card, which the catalog hangs on a
// dash before whatever qualifiers follow - "Torchic - 2004 (Chris Fulop)" -
// and worldsDeckYearRe off a deck or a code card, which name it in front.
// Every one of the 2,001 products carries it one way or the other. The tail
// pattern keeps what follows the year so stripping it leaves the qualifiers
// where the peeler can still reach them.
var (
	worldsYearTailRe = regexp.MustCompile(`\s+-\s+((?:19|20)\d{2})(\s*(?:[\(\[].*)?)$`)
	worldsDeckYearRe = regexp.MustCompile(`(?i)\b((?:19|20)\d{2})\s+world championship`)
)

// worldsYear is the championship a product belongs to, empty for a product
// that names none.
func worldsYear(name string) string {
	if m := worldsYearTailRe.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	if m := worldsDeckYearRe.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return ""
}

// worldsReleaseDate dates a championship's set. Nothing this build reads
// publishes the day: tcgdex files no World Championship set at all, and the
// catalog's own group date is 2018-07-01 for all twenty, which is when
// TCGplayer made the shelf and not when anything was printed. The
// championship is held in August every year it is held - 2004 in Orlando
// through 2025 in Anaheim, with 2020 and 2021 missing because it was not
// held - so the month is right and the day is the first, said plainly here
// rather than guessed at differently in twenty places.
func worldsReleaseDate(year string) string {
	return year + "-08-01"
}
