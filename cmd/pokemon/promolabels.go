package main

import (
	"regexp"
	"strings"

	"github.com/mtgban/datastore-gen/internal/handtable"
)

// bareNumberingRe matches a qualifier that is a number and nothing else,
// short enough not to be a year. The trainer kits are where they come
// from: TCGplayer writes a card's place in the kit into its name and its
// parent set's number into the Number field, so "Alolan Ninetales (17)"
// and "Alolan Ninetales (30)" are one card at SM128 sold in both halves of
// the box, told apart by where each sits. That is a numbering, not a
// promotion.
//
// Four digits are left alone because they are years, and a year does name
// a promotion: "No. 1 Trainer (2012)" is a trophy card of that season and
// "Professor Birch (2006)" a Player Rewards card of that one, neither
// carrying a number to say so instead.
var bareNumberingRe = regexp.MustCompile(`^#?[0-9]{1,3}$`)

// numberRangeRe matches a label that is a run of collector numbers. A
// V-UNION is four cards sold as one product, and the catalog writes the
// four numbers as a span: "Morpeko V-Union (SWSH287-290)" is SWSH287
// through SWSH290. Which numbers, not what promoted them - and a promo type
// of "swsh287290" is a numbering nobody would query for.
var numberRangeRe = regexp.MustCompile(`^[A-Z]{0,5}[0-9]{1,3}-[0-9]{1,3}$`)

// copyrightDateRe matches a label naming the year printed in a card's
// copyright line. Trick or Trade reprinted its Gengar with the line reading
// 2021 and again reading 2022, and the line is what tells the two apart, so
// the label is a mark. It is not a date the card came out: read as one, the
// year went out as the printing's release date, on a card its set already
// dates to the year after.
var copyrightDateRe = regexp.MustCompile(`(?i)^(?:19|20)[0-9]{2} copyright date$`)

// pokemonListSep matches the four ways the catalog joins a list of them.
var pokemonListSep = regexp.MustCompile(`\s*(?:,|&|/|\band\b)\s*`)

// allPokemon reports whether a qualifier is a list of Pokemon and nothing
// else.
func allPokemon(qualifier string, pokemon map[string]bool) bool {
	// "Articuno, Zapdos, & Moltres" and "Shaymin, Zeraora, and Marshadow"
	// separate twice over between the last two, so a split leaves an empty
	// piece between them. That is the separator, not a part.
	// A blister sometimes says what it holds as well as which: "[Snorlax,
	// Morpeko & Applin Cards]" is three Pokemon and the word for what they
	// are printed on.
	qualifier = strings.TrimSuffix(qualifier, " Cards")
	var named int
	for _, part := range pokemonListSep.Split(qualifier, -1) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if !pokemon[mtgmatcherNormalize(part)] {
			return false
		}
		named++
	}
	return named > 1
}

// stampPlaceRe splits the place off a stamp that carries one. Battle
// Academy stamps every card of a deck and numbers them, so "#47 Charizard
// Stamped" is the 47th card of the Charizard deck - 180 labels over 240
// printings, and behind them only three stamps: Charizard, Pikachu and
// Cinderace.
//
// The stamp is a promotion and the place in the deck is not, so the place
// comes off here and stays in the variant, where it goes on telling the
// sixty cards of a deck apart.
var stampPlaceRe = regexp.MustCompile(`^#[0-9]{1,3}\s+(\S.*)$`)

// formeRe matches a label naming which shape a Pokemon is in. A cloak is
// the shape Burmy and Wormadam take from what they last hid in: "Burmy
// (Plant Cloak)" beside its Sandy and Trash Cloaks.
var formeRe = regexp.MustCompile(`(?i)^(?:.+ forme|form [a-z]|.+ cloak)$`)

// deckPlaceRe splits a qualifier into a name and the number behind it.
var deckPlaceRe = regexp.MustCompile(`^(.+?)\s+([0-9]{1,3})$`)

// variantOnlyQuals name a label that tells two printings apart without
// saying anything promoted either, and which no rule here can recognise on
// its own. Trading Card Game Classic ships three decks - Blastoise,
// Charizard and Venusaur - and every card is in all three, so CLB, CLC and
// CLV say which deck a copy came from and nothing more. Thirteen cards
// each, and the marker is the only thing between the three copies, so it
// stays a variant rather than leaving.
var variantOnlyQuals = handtable.New("variantOnlyQuals", map[string]bool{
	"clb": true,
	"clc": true,
	"clv": true,
	// The three Champions Festival years, which their numbers already fix:
	// XY27 is the 2014 card, XY91 the 2015 and XY176 the 2016, and the
	// seven placings each carries had the same year taken off them for the
	// same reason. No other card carries a bare year of these three, and if
	// one ever does it will read as a variant here until somebody says
	// otherwise.
	"2014": true,
	"2015": true,
	"2016": true,
	// "Gym Badge (Giovanni)" is one badge of eight, each named for the
	// leader who awards it. Which badge, not what promoted it - and all
	// eight, one card name and one number between them, where only the
	// last was named here and the other seven read as promotions. The
	// guard cannot find them: they are told apart by these very labels,
	// so nothing collides for it to repair.
	// A LEGEND card is printed in two halves and sold as two cards, and
	// "Top" and "Bottom" say which half. Which piece, not what promoted
	// it - and they sit beside the placings "Top 8", "Top 16" and "Top 32",
	// which are three different achievements and stay the promotions they
	// are.
	"top":    true,
	"bottom": true,
	// The people a Supporter is named for. "Professor's Research
	// [Professor Rowan]" and its Sada, Turo, Willow, Elm, Sycamore, Kukui
	// and Birch are one card name and eight versions; "Boss's Orders
	// [Cyrus]" and its Ghetsis and Corbeau are one and three. Which
	// version, not what promoted it - the same shape as a badge, and
	// unlike a badge each has a collector number of its own, so these are
	// marks by what they are rather than by what would otherwise collide.
	"professor rowan":    true,
	"professor sada":     true,
	"professor turo":     true,
	"professor willow":   true,
	"professor elm":      true,
	"professor sycamore": true,
	"professor kukui":    true,
	"professor birch":    true,
	"cyrus":              true,
	"ghetsis":            true,
	"corbeau":            true,
	"brock":              true,
	"misty":              true,
	"lt. surge":          true,
	"erika":              true,
	"koga":               true,
	"sabrina":            true,
	"blaine":             true,
	"giovanni":           true,
	// What kind of card it is, which the game prints on the card and the
	// catalog repeats in the name: a Prime is the HeartGold SoulSilver
	// era's rare ("Absol (Prime)", at its own number), Alpha and Omega
	// are the two Ancient Traits of Primal Clash, Basic and Special the
	// two kinds of Darkness and Metal Energy, and a Supporter and a
	// Rocket's Secret Machine are Trainer subtypes. None was promoted by
	// anything, and each is which card this is where a name has two.
	"prime":                   true,
	"alpha":                   true,
	"omega":                   true,
	"basic":                   true,
	"special":                 true,
	"supporter":               true,
	"rocket's secret machine": true,
	// How a copy was printed, where the printing is what a collector is
	// buying: the Base Set Pikachu with red cheeks against the corrected
	// yellow, the Charizard with a black dot, the Wizards promo misprint,
	// and the Aquapolis and Expedition decks printed without the e-Reader
	// strip. A run of the press is not a promotion, and it is exactly a
	// mark - which copy of the number this is. These and the energy kinds
	// share their number with the plain printing, and stayed labels while
	// the loader told a plain listing from a decorated one by the label
	// alone; since 2026-09-10 it ranks an unmarked printing over a marked
	// one the same way, so "Charizard" of Base Set means the card and
	// "Charizard (Black Dot Error)" the error.
	"red cheeks":      true,
	"misprint":        true,
	"black dot error": true,
	"no e-reader":     true,
	// Which artwork. The Pikachu World Collection reprinted six Pikachu
	// in nine languages and named each by the card it reprints: the
	// Flying, Surfing, Ivy and Baby Pikachu beside the Base Set and
	// Jungle ones, which read as the sets they are.
	"flying":  true,
	"surfing": true,
	"ivy":     true,
	"baby":    true,
})

// printColors are colours a qualifier names, which is never what promoted
// a printing and is usually not even what tells it from its siblings. The
// catalog writes the colour as a gloss on something that already says which
// card this is: "Vivillon (High Plains [Orange])" beside its "(Meadow
// [Pink])", where the pattern is the difference and the colour only says
// what the pattern looks like. "Gyarados (Red)" is the shiny at its own
// number, and "Pokemon Catcher (Gold)" is a Secret Rare that says so in its
// rarity.
//
// So a colour is set aside rather than published, and comes back as the
// mark it is only where it turns out to be the whole of the difference -
// which is one printing, the green Pikachu of the World Collection sharing
// PW5 with the plain Japanese one.
var printColors = map[string]bool{
	"red": true, "blue": true, "green": true, "yellow": true, "orange": true,
	"purple": true, "pink": true, "black": true, "white": true, "brown": true,
	"gold": true, "silver": true, "bronze": true, "grey": true, "gray": true,
	"teal": true,
}

// languages a qualifier may name. The Pikachu World Collection is one
// Pikachu printed in eight of them and sold as eight cards - "Pikachu
// (Japanese)" beside its Italian, Korean, Spanish, German, French, Polish
// and Portuguese - so the language is what tells those printings apart and is
// no more a promotion than a rarity is.
//
// Only a qualifier that is a language entire counts. "Japanese Exclusive"
// names a market and "Japanese 11th Movie Commemoration Set" a set, not
// the words on the card, and both stay the promotions they are.
var languages = map[string]string{
	"japanese": "Japanese", "italian": "Italian", "korean": "Korean",
	"spanish": "Spanish", "german": "German", "french": "French",
	"portuguese": "Portuguese", "chinese": "Chinese", "polish": "Polish",
	"dutch": "Dutch", "russian": "Russian", "thai": "Thai",
}

// languageSaid reads the language a label opens with, beside what is left
// once it is taken off. Only the opening word: a label naming a language
// anywhere else is naming something else - "Japanese Version" is the
// language, "Pokemon Japan" is a place - and the catalog writes the
// language first where it writes it at all.
func languageSaid(label string) (named, rest string) {
	head, tail, found := strings.Cut(label, " ")
	if !found || tail == "" {
		return "", label
	}
	named, isLanguage := languages[strings.ToLower(head)]
	if !isLanguage {
		return "", label
	}
	return named, tail
}
