package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/handtable"
)

// tcgdexSet is the slice of a tcgdex set this build reads.
type tcgdexSet struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ReleaseDate string `json:"releaseDate"`
	Symbol      string `json:"symbol"`
	Serie       struct {
		ID string `json:"id"`
	} `json:"serie"`
}

// tcgdexSymbolURL turns the bare path the API hands back in Symbol into one
// that actually answers. The field is always "https://assets.tcgdex.net/univ/<series>/<id>/symbol",
// and for the first nine days this shipped, appending an extension there
// worked - .webp answered, and so did .png. It stopped: as of this build,
// nothing under univ/ resolves in any extension, and tcgdex's own asset
// docs (https://tcgdex.dev/assets) describe a locale-scoped path instead,
// "en/<series>/<id>/symbol.png" - no webp, no universal. This build already
// reads everything else tcgdex serves through en (card images, notably),
// so univ is swapped for it here rather than left as the vendor's own word
// for where the asset lives.
//
// tcgdex still claims a symbol for some sets it does not serve one for
// under this path either - 21 of the 169 it names one for, mostly the
// trainer-kit half-decks - the same shape as a claimed id resolving to a
// 404 elsewhere in this build. Those carry none, same as before.
func tcgdexSymbolURL(raw string) string {
	return strings.Replace(raw, "/univ/", "/en/", 1) + ".png"
}

// tcgdexCard is the slice of a tcgdex card this build reads.
type tcgdexCard struct {
	ID       string   `json:"id"`
	LocalID  string   `json:"localId"`
	Name     string   `json:"name"`
	Rarity   string   `json:"rarity"`
	Category string   `json:"category"`
	Types    []string `json:"types"`
	Image    string   `json:"image"`
	Variants struct {
		Normal       bool `json:"normal"`
		Reverse      bool `json:"reverse"`
		Holo         bool `json:"holo"`
		FirstEdition bool `json:"firstEdition"`
	} `json:"variants"`
	Set struct {
		ID string `json:"id"`
	} `json:"set"`
}

// pokemonTypes are the types a Pokemon prints, as tcgdex spells them.
var pokemonTypes = []string{"Colorless", "Darkness", "Dragon", "Fairy", "Fighting", "Fire", "Grass", "Lightning", "Metal", "Psychic", "Water"}

// typeSpellings are the catalog's other words for a type: the video games'
// names, and its typos.
var typeSpellings = handtable.New("typeSpellings", map[string]string{
	"Dark":      "Darkness",
	"Electric":  "Lightning",
	"Lighnting": "Lightning",
	"Normal":    "Colorless",
})

// printedTypes elects the types a Pokemon prints, published as "types":
// tcgdex's where tcgdex knows the card, else the catalog's Card Type where
// that names types and nothing else. The two agree on 29,381 of the 29,438
// printings both type. A Trainer or an Energy card has none; tcgdex types
// some Energy and not others.
type printedTypes struct {
	fromTcgdex  int
	fromCatalog int
}

func (c *printedTypes) of(dex *tcgdexCard, cardType string) []string {
	if dex != nil && dex.Category != "" && dex.Category != "Pokemon" {
		return nil
	}
	// A type listed twice is a typo for another, as on Dark Houndoom's
	// Darkness and Darkness, so the catalog answers instead
	if dex != nil && len(dex.Types) > 0 && len(slices.Compact(slices.Sorted(slices.Values(dex.Types)))) == len(dex.Types) {
		c.fromTcgdex++
		return dex.Types
	}
	var types []string
	for _, word := range strings.FieldsFunc(cardType, func(r rune) bool { return r == ' ' || r == '/' }) {
		spelled, hand := typeSpellings.Get(word)
		if hand {
			word = spelled
		}
		if !slices.Contains(pokemonTypes, word) {
			return nil
		}
		if !slices.Contains(types, word) {
			types = append(types, word)
		}
	}
	if len(types) > 0 {
		c.fromCatalog++
	}
	return types
}

// loadTcgdex reads a raw GraphQL response envelope from a file when a path
// is given, or POSTs the query to the live endpoint, retried and behind the
// last-good cache. The API sits behind a single host, where a stale DNS
// record once kept the nightly dialing a dead server for three days.
func loadTcgdex(path, cacheDir, cacheName, query string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	return emit.Upstream{
		Source:    "tcgdex",
		Check:     readsAsEnvelope,
		CacheDir:  cacheDir,
		CacheName: cacheName,
	}.Post(tcgdexGraphQLURL, body)
}

// readsAsEnvelope says whether a tcgdex response decodes as a GraphQL
// envelope carrying data and no errors, which is what makes it worth
// keeping.
func readsAsEnvelope(data []byte) error {
	var raw json.RawMessage
	return decodeEnvelope(data, &raw)
}

// readsAsSets says whether a pokemontcg.io response decodes as a list of
// sets with something in it.
func readsAsSets(data []byte) error {
	var payload struct {
		Data []pokemontcgSet `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if len(payload.Data) == 0 {
		return errors.New("the response lists no sets")
	}
	return nil
}

// decodeEnvelope unwraps a GraphQL response: any errors key is a hard
// failure, a partial answer being worse than none.
func decodeEnvelope(data []byte, into any) error {
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	err := json.Unmarshal(data, &env)
	if err != nil {
		return err
	}
	if len(env.Errors) > 0 && string(env.Errors) != "null" {
		return fmt.Errorf("graphql errors: %s", env.Errors)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return errors.New("graphql response carries no data")
	}
	return json.Unmarshal(env.Data, into)
}

// tcgdexSymbols writes the marks tcgdex prints on a name the way TCGplayer
// writes them. The catalog spells every one of them out and carries not a
// single symbol in 32,675 product names - "Alakazam Star", "Giratina Prism
// Star", "Aerodactyl (Delta Species)" - while tcgdex prints the mark: 192
// cards carry δ, 27 carry ◇, 24 carry ☆ or ★, 36 carry ♀ or ♂.
//
// The catalog's spelling is the one this datastore speaks. TCGplayer is
// what prices a card and what a storefront copies its wording from, so a
// listing will say "Star", never "☆", and the name a listing is matched
// against should be the one it will be written with. These only ever enter
// through minting, which is the minority path.
//
// Accents need no rule: the matcher folds é to e already, so "Flabébé" and
// "Flabebe" are one name to it whatever is stored.
var tcgdexSymbols = strings.NewReplacer(
	"☆", " Star",
	"★", " Star",
	"◇", " Prism Star",
	"♀", " F",
	"♂", " M",
)

// deltaRe matches the mark tcgdex hangs off a Delta Species card's name,
// which the catalog writes as a parenthetical instead.
var deltaRe = regexp.MustCompile(`\s*δ`)

// catalogSpelling is a name written the way the catalog would write it.
func catalogSpelling(name string) string {
	name = deltaRe.ReplaceAllString(name, " (Delta Species)")
	name = tcgdexSymbols.Replace(name)
	return strings.Join(strings.Fields(name), " ")
}

// identityKey is a name and number reduced to what makes two spellings of
// one card the same card: the catalog's marks, no case, no punctuation.
// It is what the mint asks before adding a card, and the answer has to be
// insensitive to every way the two sources differ in writing it down.
func identityKey(name, number string) string {
	folded := strings.ToLower(catalogSpelling(name))
	var out strings.Builder
	for _, r := range folded {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String() + "|" + strings.ToLower(strings.TrimSpace(number))
}

// renumberedSets are the tcgdex sets that hold the catalog's own cards
// under a numbering of their own, so the number cannot be asked and the
// name has to answer alone. Named rather than detected: a set-wide
// numbering disagreement looks exactly like a set of genuinely new cards
// until someone reads both, and every rule tried for telling them apart
// refused real printings.
//
// Celebrations reprints twenty-five classics and numbers them CC001-CC025
// where the catalog numbers each by the card it reprints - Blastoise is
// CC001 upstream and 2 here, Umbreon Star CC015 and 17. All twenty-five
// were minted a second time, unpriced, beside the products that carry
// their prices.
var renumberedSets = handtable.New("renumberedSets", map[string]bool{
	"cel25cc": true, // Celebrations: Classic Collection
})

// shadowShare is how much of a tcgdex set has to be the catalog's already
// for the set to be the catalog's under another name. The measured sets sit
// at 67% and above or at 33% and below, so anything in the gap separates
// them; half is the middle of it and nothing sits near either side.
const shadowShare = 0.5
