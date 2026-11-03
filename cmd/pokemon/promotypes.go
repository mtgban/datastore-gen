package main

import (
	"strings"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/vocabulary"
)

// published is what this datastore states in fields of its own, which a
// label is read against before it is taken for a promotion.
type published struct {
	// pokemon are the Pokemon, by normalized name.
	pokemon map[string]bool
	// setNames are the sets, under every spelling a label writes one in.
	setNames map[string]bool
	// tails are the labels the catalog writes on their own, and the ones an
	// over-long label leaves behind once its set or Pokemon comes off. A
	// shorter label splits only into one of these, so "Team Up Stamped"
	// splits the way "Prismatic Evolutions Stamped" does and "Fossil Museum
	// Exclusive" stays the promotion it is.
	tails map[string]bool
}

// promoTypesOf folds a printing's surviving qualifiers to the spelling the
// matcher declares tags in.
func promoTypesOf(s *single, p published, onShelf bool, own map[string]bool, finish, mark string) (kept, left []string, year, found, spoken string) {
	pokemon, setNames := p.pokemon, p.setNames
	// A code card is a redemption slip, and its label names what the code
	// unlocks or the box it came in: "Code Card - Steam Siege Collectible
	// Pin 3 Pack Blister [Shiny Mega Gardevoir]", "[Ballonea Gym]",
	// "[Chespin Box]". Nothing promoted the slip. Seventy labels were in the
	// promo types for no other reason, none of them shared with a card
	// anybody plays, and each still tells its own slip from the others as a
	// variant.
	if s.product.Extended("Rarity") == codeCardRarity {
		// Nothing promoted a slip, but which slip it is has to survive:
		// "Code Card - Evolving Skies Single Pack Blister" is sold with an
		// Eevee on it and with a Galarian Slowpoke, one name and one rarity
		// between them. The label is the mark it is, wherever it appears
		// rather than only where the guard asks - a slip nothing else is
		// sold beside came out of one blister just the same.
		for _, q := range s.quals {
			left = append(left, strings.ToLower(q.text))
			if found == "" {
				found = strings.ToLower(q.text)
			}
		}
		return nil, left, "", found, ""
	}
	// What is left out is handed back rather than forgotten: a label that
	// says nothing about what promoted a printing may still be the only
	// thing telling it from a sibling, and markPrintings puts those back as
	// the mark they are.
	out := make([]string, 0, len(s.quals))
	for _, q := range s.quals {
		lowered := strings.ToLower(q.text)
		if mark != "" && lowered == mark {
			continue
		}
		// A qualifier that tells two printings apart without saying
		// anything promoted either is a mark, and is one wherever it
		// appears rather than only where the guard asks for it: CLB, CLC
		// and CLV say which of the three Classic decks a copy came from,
		// and a copy that happens to need no telling apart came from one
		// just the same.
		if printColors[lowered] {
			left = append(left, lowered)
			continue
		}
		if named, isLanguage := languages[lowered]; isLanguage {
			if spoken == "" {
				spoken = named
			}
			continue
		}
		// A label that opens with a language says in a token what the
		// language field says on its own: "Japanese Meiji Chocolate
		// Exclusive Promo" is that promotion, printed in Japanese. The
		// language comes off and what promoted the printing keeps the
		// token.
		if named, rest := languageSaid(q.text); named != "" {
			if spoken == "" {
				spoken = named
			}
			// A language and the word exclusive says which language and
			// nothing else: "Japanese Exclusive" is the Japanese printing,
			// which the language field now says on its own.
			if strings.EqualFold(rest, "Exclusive") {
				continue
			}
			q.text, lowered = rest, strings.ToLower(rest)
		}
		// A label saying what the finish field says of this printing names
		// no promotion. It is which copy of the number this is where two
		// products share one: "Armaldo ex (Holo)" and "Armaldo ex
		// (Non-Holo)" are two products at 016/017 of POP Series 1, told
		// apart by nothing but the finish each is priced as, and a listing
		// naming either has to reach the one it names.
		if saysFinish(emit.PromoSlug(q.text), finish) {
			left = append(left, lowered)
			if found == "" && mark == "" {
				found = lowered
			}
			continue
		}
		if variantOnlyQuals.Has(lowered) {
			if mark == "" {
				// Two of these on one printing are two facts of one
				// copy, the way markPrintings already joins them:
				// "Darkness Energy (No E-Reader) (Special)" is the
				// special energy from the deck printed without the
				// strip, and read one at a time the second went out as
				// a promotion.
				if found == "" {
					found = lowered
				} else {
					found += " " + lowered
				}
				continue
			}
			// Another mark holds the slot - a World Championship player on
			// "Boss's Orders [Cyrus]" from somebody's deck - and a printing
			// wears one. Rather than lose which version it is, it stays a
			// label here, which is where it was before any of this.
			out = append(out, emit.PromoSlug(q.text))
			continue
		}
		// A label naming the set the card already sits in restates the
		// set field, the way a number in a label restates the number:
		// "Bagon (Black Star Promos)" sits in Nintendo Promos, which is
		// the Nintendo Black Star Promos.
		if own[mtgmatcherNormalize(q.text)] {
			left = append(left, lowered)
			continue
		}
		// Which set a promo reprints. The promo shelves hold a card from
		// everywhere - "Sceptile" 010 reprinted out of EX Emerald beside
		// the same number out of DP Stormfront - and the set it came from
		// is which copy this is rather than what promoted it. A shelf
		// writes the word promo after the set now and then, "Best of Game
		// Promo" for a jumbo of the Best of Game card, and says no more by
		// it.
		if label := strings.TrimSuffix(q.text, " Promo"); onShelf && namesASet(label, setNames) {
			left = append(left, strings.ToLower(label))
			if found == "" && mark == "" {
				found = strings.ToLower(label)
			}
			continue
		}
		// What the card pictures, and which shape it is in. Neither
		// promoted anything - a Pokemon on a promo says which artwork or
		// which deck a copy came from, and a forme says which shape the
		// Pokemon is in - so each is the mark of a copy rather than a
		// promotion, and is published as one wherever it appears.
		//
		// Which shape the Pokemon is in, which no card was promoted for:
		// Deoxys in its Normal, Attack, Defense and Speed Formes at four
		// numbers of its own, Shaymin Lv.X in its Land and Sky, and the two
		// M Charizard EX that say Form X and Form Y where the four others
		// say a bare X and Y this already leaves out.
		if formeRe.MatchString(q.text) {
			left = append(left, lowered)
			if found == "" {
				found = lowered
			}
			continue
		}
		if pokemon[mtgmatcherNormalize(q.text)] || bareNumberingRe.MatchString(q.text) ||
			numberRangeRe.MatchString(q.text) || copyrightDateRe.MatchString(q.text) {
			left = append(left, lowered)
			if found == "" {
				found = lowered
			}
			continue
		}
		// A Pokemon and a number is a deck and a place in it. Battle
		// Academy names its decks after Pokemon and numbers the cards
		// within each - "Armarouge ex (Armarouge 60)" is card 60 of the
		// Armarouge deck, and the card's own number is 105.
		if m := deckPlaceRe.FindStringSubmatch(q.text); m != nil && pokemon[mtgmatcherNormalize(m[1])] {
			left = append(left, lowered)
			if found == "" {
				found = lowered
			}
			continue
		}
		// And a list of them is still them. A blister naming what is inside
		// it - "Glaceon/Vaporeon/Sylveon/Espeon", "Articuno, Zapdos, &
		// Moltres", "Mew & Mewtwo" - names 25 labels no two cards share,
		// and every one is a variant for the same reason a single Pokemon
		// is. Every part has to be a Pokemon before any of it is taken.
		if allPokemon(q.text, pokemon) {
			left = append(left, lowered)
			if found == "" {
				found = lowered
			}
			continue
		}
		text := q.text
		// The facts this one label states beside its promotion, which
		// together are the mark of the copy: the place in a deck and the
		// deck, "#42 Charizard Stamped" being card 42 of the Charizard
		// deck. Read one at a time the place was a mark only where the
		// guard restored it, and Charmander 18/147 sits in that deck at
		// four places, three of them Promo and one Common, so the Common
		// one collided with nothing and went out marked with the deck
		// alone - which a listing naming any of the four places named.
		var stated []string
		if m := stampPlaceRe.FindStringSubmatch(text); m != nil {
			// The stamp is the promotion and the place in the deck is the
			// mark: Battle Academy stamps all sixty cards of a deck alike,
			// so "#1 Charizard Stamped" and its "#16" are one promotion
			// and two printings.
			text = m[1]
			place := strings.TrimPrefix(strings.Fields(q.text)[0], "#")
			left = append(left, place)
			stated = append(stated, place)
		}
		if m := runSeries.FindStringSubmatch(text); m != nil {
			// A label that is the instalment and nothing else says only
			// which running: "Fezandipiti ex (Series 7)" sits in a set
			// already named Prize Pack Series Cards, and "series" on its
			// own named no promotion. The instalment is the mark, and the
			// loader now reads a listing naming no series as the
			// unlabelled printing on its own.
			if strings.EqualFold(m[1], "series") {
				left = append(left, lowered)
				if found == "" {
					found = lowered
				}
				continue
			}
			text = m[1]
			if found == "" {
				found = "series " + m[2]
			}
		}
		if rest, stated := promoYear(text); stated != "" {
			year, text = stated, rest
			if text == "" {
				continue
			}
		}
		// A label that is a set and nothing else is which set this
		// reprints, whole. The shelf test above asks the same thing of
		// the shelves that hold a card from everywhere; this asks it of
		// a label too long to be a promotion's name wherever it sits.
		// Only of one too long: "Gym Challenge" is a set of 2000 and the
		// tournament tier it is named for on the promo shelves, and read
		// as the set it would mark three promotions with a reprint that
		// never happened.
		if len(emit.PromoSlug(text)) > promoTypeLimit && namesASet(text, setNames) {
			left = append(left, strings.ToLower(text))
			if found == "" && mark == "" {
				found = strings.ToLower(text)
			}
			continue
		}
		// A label opening with a fact this datastore already publishes is
		// that fact with a promotion stuck to it: "Prismatic Evolutions
		// Stamped" is the set the promo reprints beside the stamp, and
		// "Alolan Ninetales Half Deck" the Pokemon beside the deck. The
		// fact becomes the mark it is everywhere else and the promotion
		// keeps the token.
		//
		// Past promoTypeLimit the split is forced, because the token would
		// be no use whole. Under it the split happens only where what is
		// left is a promotion in its own right - a label the catalog writes
		// on its own, or one the forced splits leave behind - so that "Team
		// Up Stamped" splits the way "Prismatic Evolutions Stamped" does
		// and "XY Evolutions Prerelease", whole at exactly the limit, the
		// way the longer prereleases do, while "Fossil Museum Exclusive" is
		// not read as the 1999 set with a museum stuck to it and "Black
		// and White Tour Promo" not as the set with a tour. A stem left
		// with only the word promo behind it was the set's promo and says
		// no more: "Best of Game Promo" is a jumbo of the Best of Game
		// card.
		if stem, rest := publishedPrefix(text, pokemon, setNames); rest != "" &&
			(len(emit.PromoSlug(text)) > promoTypeLimit || p.tails[rest] || strings.EqualFold(rest, "Promo")) {
			left = append(left, strings.ToLower(stem))
			stated = append(stated, strings.ToLower(stem))
			text = rest
		}
		if len(stated) > 0 && found == "" && mark == "" {
			found = strings.Join(stated, " ")
		}
		if strings.EqualFold(text, "Promo") {
			continue
		}
		out = append(out, emit.PromoSlug(text))
	}
	return out, left, year, found, spoken
}

// promoTypeLimit is how long a promo type may read before it stops being a
// name and starts being a sentence. A query carries a token, and a token
// nobody can type is one nobody will.
const promoTypeLimit = vocabulary.TokenLimit

// publishedPrefix is the run of words a label opens with that this datastore
// states elsewhere - the set a promo reprints, or the Pokemon it pictures -
// beside what is left once it is taken off. The longest such run is read, so
// that a set whose name opens with a Pokemon's is read as the set.
func publishedPrefix(label string, pokemon, setNames map[string]bool) (stem, rest string) {
	fields := strings.Fields(label)
	for i := len(fields) - 1; i > 0; i-- {
		head := strings.Join(fields[:i], " ")
		if namesASet(head, setNames) || pokemon[mtgmatcherNormalize(head)] {
			return head, strings.Join(fields[i:], " ")
		}
	}
	return "", ""
}

// pokemonNames are the Pokemon tcgdex files as Pokemon, 2,842 of them
// against its 3,055 Trainers and 568 Energies. A qualifier naming one is a
// variant and not a promo type: "Ditto - 039/113 (Pikachu)" is the Ditto
// transformed into Pikachu, "Energy Search (Latias)" is the Latias artwork,
// and neither card was promoted by anything. 420 labels over 690 printings
// were filed as promotions for being named after a Pokemon.
//
// The category is what makes this safe to do by name. "Poke Ball", "Dusk
// Ball" and "Love Ball" are card names too and they say which pattern a
// reverse holo wears, and tcgdex files them as Trainers, so none of them is
// touched here. "Cyrus", "Ghetsis" and "Professor Rowan" are Trainers too
// and are equally untouched by this - they are marks, named one by one in
// variantOnlyQuals, because they say which version of one Supporter a
// printing is.
func pokemonNames(cards []tcgdexCard) map[string]bool {
	out := map[string]bool{}
	for i := range cards {
		if cards[i].Category != "Pokemon" || cards[i].Name == "" {
			continue
		}
		out[mtgmatcherNormalize(cards[i].Name)] = true
	}
	return out
}
