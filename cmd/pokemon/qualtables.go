package main

import (
	"github.com/mtgban/datastore-gen/internal/handtable"
)

// qualSplits name a qualifier the catalog wrote as one label and every
// other card writes as two. "Finneon - SWSH240 (Prerelease Staff)" is the
// only card of 298 staff printings to run the two words together, and the
// three Non-Holos are a surface and a distribution said in one breath.
var qualSplits = handtable.New("qualSplits", map[string][]string{
	"prerelease staff": {"Prerelease", "Staff"},
	// A rarity and a treatment: "Mewtwo EX (163 Secret Full Art)" is the
	// secret rare and it is full art, and both are labels hundreds of cards
	// carry on their own.
	"secret full art": {"Secret", "Full Art"},
	// A store and a surface, the way its Non-Holo siblings already read.
	"target non-holo":             {"Target Promo", "Non-Holo"},
	"non-holo dvd promo":          {"Non-Holo", "DVD Promo"},
	"non-holo gamestop exclusive": {"Non-Holo", "GameStop Exclusive"},
	"non-holo movie exclusive":    {"Non-Holo", "Movie Exclusive"},
	// A store and what it sold: folding this to the store alone dropped the
	// metalcard token the five other metal cards carry.
	"gamestop metal card": {"GameStop Exclusive", "Metal Card"},
})

var qualSpellings = handtable.New("qualSpellings", map[string]string{
	"player reward": "Player Rewards",
	// One marking, named twice. Both cards carry the anniversary logo, and
	// the catalog calls it a stamp on one of them: "Professor Burnet -
	// SWSH167 (25th Anniversary Stamp)" against "Pikachu - 58/102 (25th
	// Anniversary)". The bare form is what its own family uses, the catalog
	// writing "10th Anniversary" and "20th Anniversary" beside it.
	"25th anniversary stamp": "25th Anniversary",
	// One marking, named twice, one use each: "Larvitar (Delta Species
	// Stamp)" against "Ditto - 64/113 (Squirtle) (Delta Species Stamped)".
	// "Stamped" is what the rest of the catalog says - Charizard Stamped,
	// Pikachu Stamped, Chaos Rising Stamped.
	"delta species stamp": "Delta Species Stamped",
	// One storefront naming its exclusive two ways, thirteen times and
	// four: "Flapple - 022/192 (EB Games Exclusive)" against "Lechonk -
	// 154/198 (EB Games Promo)".
	//
	// "EB Games Exclusive APAC" reads like a third and is not. TCGplayer
	// sells two Flapples at 022/192, one under each label, so the region is
	// the only thing between them - fold it and validate refuses the build
	// for two products wearing one identity, which is how this was found.
	"eb games promo": "EB Games Exclusive",
	"eb games":       "EB Games Exclusive",
	// A convention names itself with and without the year it was held and
	// the word promo: "Treecko - 70/106 (GEN CON)" against "Bagon - 50/97
	// (Gen Con 2004 Promo)". A promo type saying "promo" says nothing.
	"gen con":            "Gen Con",
	"gen con 2004 promo": "Gen Con",
	// And a cereal company, the same way: "Pikachu - SM04 (General Mills)"
	// against "Litten - SM02 (General Mills Promo)".
	"general mills promo": "General Mills",
	// One surface, three labels: eighteen "Holo", eight "Holo Common" and
	// three "Holofoil". The rarity says common where a card is common, so
	// the label only has to say holo.
	"holofoil":    "Holo",
	"holo common": "Holo",
	// "Ancient Mew (Japanese Exclusive Print)" against "Gardevoir ex
	// (Japanese Exclusive)". The ones naming a set beside them - "SM-P
	// Japanese Exclusive", "Vstar Universe Japanese Exclusive" - name a
	// different thing and stay.
	"japanese exclusive print": "Japanese Exclusive",
	// A player misspelled once out of twenty-four. Naoto Suzuki played the
	// 2017 World Championships and twenty-three of his cards say so; the
	// Wimpod says Naoko.
	"naoko suzuki": "Naoto Suzuki",
	// A label saying "promo" says nothing this field does not, so where the
	// catalog offers both the bare form wins.
	"national championship promo": "National Championships",
	"premium collection promo":    "Premium Collection",
	"thank you promo":             "Thank You",
	"toys r us promo":             "Toys R Us",
	"pokemon day stamped":         "Pokemon Day",
	// The kit is where the prerelease card came from, which is what
	// "Prerelease" already says on the other 327.
	"prerelease kit exclusive": "Prerelease",
	// One code card naming the deck's Urshifu with its VMAX and one
	// without.
	"single strike urshifu vmax": "Single Strike Urshifu",
	// More of the same: a season, a shelf, a store, a surface, each named
	// twice. Where the catalog offers a bare form the word "promo" comes
	// off it, and where it does not the fuller name is the one that says
	// something.
	"2006-2007 league promo":      "2006-2007",
	"2011 pokemon league promo":   "2011 Pokemon League",
	"black bolt":                  "Black Bolt Stamped",
	"black star":                  "Black Star Promos",
	"gamestop":                    "GameStop Exclusive",
	"gamestop promo":              "GameStop Exclusive",
	"movie exclusive":             "Movie",
	"movie promo":                 "Movie",
	"snap promo":                  "Snap",
	"state championship promo":    "State Championships",
	"store exclusive promo":       "Store Exclusive",
	"store promo":                 "Store Exclusive",
	"prismatic evolution stamped": "Prismatic Evolutions Stamped",
	// Two players misspelled once each, against nineteen and twenty-seven
	// cards that spell them right.
	// The catalog abbreviates the mark on nine cards and writes it out on
	// 361: "Dustox (8 Delta)" against "Aerodactyl (Delta Species)".
	"delta": "Delta Species",
	// Two Pokemon the catalog misspells, which the rule above cannot see
	// as Pokemon until they are spelled the way one is.
	"rowlett":             "Rowlet",
	"galarian slowbrow v": "Galarian Slowbro V",
	// A tier, a set, a convention, a programme and a championship, each
	// named twice.
	"league":                      "League Promo",
	"surging sparks":              "Surging Sparks Stamped",
	"san diego comic con":         "SDCC Stamp",
	"secret shining":              "Secret",
	"play! pokemon promo":         "Play! Pokemon",
	"regional championship promo": "Regional Championships",
	"regional stamp promo":        "Regional Championships",
	// The year is on seven of the eight and not on the eighth, and nothing
	// here knows it belongs there, so the eight read as the seven do
	// without it rather than the one being given a year it may not have.
	"wotc 2002 league promo": "WotC League Promo",
	// The Champions Festival placings, whose year the number already
	// fixes: XY27 is the 2014 card, XY91 the 2015 and XY176 the 2016, and
	// each carries seven placings that say the year again. Nine year-less
	// placings already sit beside them.
	//
	// A table rather than a rule, because a year in front is only redundant
	// where something else says it. "2010 Play! Pokemon" and "2011 Pokemon
	// League" sit on energies with no number at all, and dropping their
	// year would drop the only season they name.
	"2014 champion":         "Champion",
	"2014 finalist":         "Finalist",
	"2014 quarter finalist": "Quarter-Finalist",
	"2014 semi-finalist":    "Semi-Finalist",
	"2014 staff":            "Staff",
	"2014 top 16":           "Top 16",
	"2014 top 32":           "Top 32",
	"2015 champion":         "Champion",
	"2015 finalist":         "Finalist",
	"2015 quarter finalist": "Quarter-Finalist",
	"2015 semi-finalist":    "Semi-Finalist",
	"2015 top 16":           "Top 16",
	"2015 top 32":           "Top 32",
	"2016 champion":         "Champion",
	"2016 finalist":         "Finalist",
	"2016 quarter finalist": "Quarter-Finalist",
	"2016 semi-finalist":    "Semi-Finalist",
	"2016 top 16":           "Top 16",
	"2016 top 32":           "Top 32",
	// A storefront and a stamp, each named with and without the word that
	// says so. "Flutter Mane - 097 (Pokemon Center)" and "Lechonk (Pokemon
	// Center Exclusive)" are one distribution; the Paldean pair missed the
	// stamp rule because the bare one has no "Stamp" to match on.
	//
	// "Pokemon Center NY" stays apart. That is the shop on Rockefeller
	// Plaza, not the chain.
	"pokemon center":             "Pokemon Center Exclusive",
	"best buy":                   "Best Buy Exclusive",
	"paldean fates":              "Paldean Fates Stamped",
	"jeremy moran":               "Jeremy Maron",
	"jose cruz galindo-rosendiz": "Jose Cruz Galindo-Resendiz",
})

// rawNames repair a product name the catalog wrote in a shape nothing can
// read, keyed by the product id it never reuses. Only four names in 32,675
// close a bracket they never opened and three of those are sealed, so this
// is a repair rather than a rule: the peeler should not be taught to guess
// where a missing parenthesis went.
var rawNames = handtable.New("rawNames", map[int]string{
	// The catalog never closes the parenthesis: "Chesnaught - XY68
	// (Prerelease [Staff]". Both qualifiers are real and neither is
	// reachable while the name is unbalanced.
	108598: "Chesnaught - XY68 (Prerelease) [Staff]",
	// A stray letter after the closing parenthesis, which stops the peel
	// dead: "Jet Energy - 2023 (Gabriel Fernandez)a".
	541801: "Jet Energy - 2023 (Gabriel Fernandez)",
	// The catalog loses the slash of the number: "Makuhita - 56109" for
	// 56/109, one digit more than a dash number may have.
	87125: "Makuhita - 56/109",
})
