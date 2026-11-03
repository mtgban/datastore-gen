package main

import (
	"strings"

	"github.com/mtgban/datastore-gen/internal/handtable"
)

// normalizeName reduces a set name to the lowercase alphanumeric words two
// spellings of it share: "é" flattens to "e" ("Pokémon GO"), the word "and"
// goes the way "&" already does ("EX Ruby and Sapphire" meets "Ruby &
// Sapphire"), "energies" and "energy" agree, and a trailing "Base Set" is
// shed while something remains ("XY Base Set" meets "XY", tcgdex's
// "Expedition Base Set" meets "Expedition", "Base Set" itself stays whole).
func normalizeName(name string) string {
	name = strings.ReplaceAll(strings.ToLower(name), "é", "e")
	words := strings.FieldsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	kept := words[:0]
	for _, w := range words {
		if w == "and" {
			continue
		}
		if w == "energies" {
			w = "energy"
		}
		kept = append(kept, w)
	}
	if len(kept) > 2 && kept[len(kept)-2] == "base" && kept[len(kept)-1] == "set" {
		kept = kept[:len(kept)-2]
	}
	return strings.Join(kept, "")
}

// setAliases maps the group names whose tcgdex counterpart is spelled from
// a different root no mechanical strip reaches — TCGplayer's promo sets
// against tcgdex's "Black Star Promos" family, the flagship base sets, and
// the sets TCGplayer names by their subtitle alone.
// Values are tcgdex set names, normalized at join time like everything else.
var setAliases = handtable.New("setAliases", map[string]string{
	"WoTC Promo":                       "Wizards Black Star Promos",
	"Nintendo Promos":                  "Nintendo Black Star Promos",
	"Diamond and Pearl Promos":         "DP Black Star Promos",
	"HGSS Promos":                      "HGSS Black Star Promos",
	"Black and White Promos":           "BW Black Star Promos",
	"XY Promos":                        "XY Black Star Promos",
	"SM Promos":                        "SM Black Star Promos",
	"SWSH: Sword & Shield Promo Cards": "SWSH Black Star Promos",
	"SV: Scarlet & Violet Promo Cards": "SVP Black Star Promos",
	"ME: Mega Evolution Promo":         "MEP Black Star Promos",
	"SM Base Set":                      "Sun & Moon",
	"SV: Scarlet & Violet 151":         "151",
	"Rumble":                           "Pokémon Rumble",
})

// stripSetPrefix removes the leading short-code TCGplayer prefixes group
// names with ("SWSH07: Evolving Skies", "SM - Guardians Rising"): a single
// alphanumeric token of at most 8 characters, one dot allowed ("SWSH12.5"),
// ahead of ":" or " - ". A longer or multi-word head ("HGSS Trainer Kit:
// Gyarados & Raichu") is a name, not a code, and stays.
func stripSetPrefix(name string) string {
	token, rest := "", ""
	colon := strings.Index(name, ":")
	dash := strings.Index(name, " - ")
	if colon >= 0 {
		token, rest = name[:colon], name[colon+1:]
	} else if dash >= 0 {
		token, rest = name[:dash], name[dash+3:]
	} else {
		return name
	}
	if len(token) == 0 || len(token) > 8 || strings.Count(token, ".") > 1 {
		return name
	}
	for _, r := range token {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && r != '.' {
			return name
		}
	}
	return strings.TrimSpace(rest)
}

// shelfNames are the groups that are a shelf rather than a set, beside the
// Deck and Blister Exclusives the name already says: each holds a reprint
// from anywhere in the game and writes which set it came from beside the
// number, so a label there naming a set this datastore carries is where the
// card was reprinted from and not what promoted it. Twelve Burger King
// promos write "[Diamond & Pearl]" and twelve "[Platinum]", and fifty jumbo
// cards name their source and nothing else; none of it is a promotion.
var shelfNames = handtable.New("shelfNames", map[string]bool{
	"Jumbo Cards":                     true,
	"Burger King Promos":              true,
	"Miscellaneous Cards & Products":  true,
	"Pikachu World Collection Promos": true,
})

// eraHeads are the series a set name or a label opens with, which the
// catalog writes in front of a set's name where this datastore's own name
// does not - "DP Legends Awakened" for our "Legends Awakened", "HGSS
// Undaunted" for our "Undaunted" - or leaves off where ours carries it:
// "Ruby & Sapphire" for our "EX Ruby and Sapphire". They used to be any
// word of four letters or fewer, which read "With" as an era in front of
// "Pokemon GO" and "Best" in front of "of Game".
var eraHeads = map[string]bool{
	"wotc": true, "ex": true, "dp": true, "dppt": true, "pl": true, "hgss": true,
	"hs": true, "bw": true, "xy": true, "sm": true, "swsh": true, "sv": true,
	"sve": true, "me": true, "mee": true, "pop": true,
}

// eraOf splits the era off a name that opens with one, and hands back
// nothing where the name does not, or where nothing but a word or two would
// be left - so nothing reads the first word of a two-word set as a prefix
// of the second.
func eraOf(name string) (era, rest string) {
	head, rest, found := strings.Cut(name, " ")
	if !found || !eraHeads[strings.ToLower(strings.TrimSuffix(head, ":"))] {
		return "", ""
	}
	if !strings.Contains(rest, " ") && len(rest) < 4 {
		return "", ""
	}
	return head, rest
}

// setNamed is the spelling under which a label names one of the sets this
// datastore carries, or "" where it names none. The label is read whole and
// then past its era: "DP Legends Awakened" against "Legends Awakened",
// "HGSS Undaunted" against "Undaunted", beside an "EX Hidden Legends" that
// matches whole.
func setNamed(label string, setNames map[string]bool) string {
	if key := mtgmatcherNormalize(label); setNames[key] {
		return key
	}
	if _, rest := eraOf(label); rest != "" {
		if key := mtgmatcherNormalize(rest); setNames[key] {
			return key
		}
	}
	return ""
}

// namesASet reports whether a label names one of the sets this datastore
// carries.
func namesASet(label string, setNames map[string]bool) bool {
	return setNamed(label, setNames) != ""
}

// latinAccents fold to the letter underneath. Only one of these is load
// bearing: tcgdex writes "Pok\u00e9dex" where the catalog writes "Pokedex",
// 175 names against 2, and a key that drops the accent rather than folding
// it made "pokdex" of one and "pokedex" of the other - so upstream's
// "Pok\u00e9Dex (HANDY909)" never reached the card the catalog sells. The rest
// are here so the next one costs nothing.
var latinAccents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c",
)

// mtgmatcherNormalize reduces a set name to its letters and digits, so the
// two sources' punctuation and casing do not part names that are the same.
func mtgmatcherNormalize(name string) string {
	// The word joining two halves of a name is dropped, whichever of the
	// three ways it is written. Our own set names use all of them -
	// "HeartGold SoulSilver" with nothing, "Diamond & Pearl" with an
	// ampersand, "EX Ruby and Sapphire" with the word - so a label writing
	// one missed a set writing another.
	var out strings.Builder
	for _, field := range strings.Fields(latinAccents.Replace(strings.ToLower(name))) {
		if field == "and" || field == "&" {
			continue
		}
		for _, r := range field {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}
