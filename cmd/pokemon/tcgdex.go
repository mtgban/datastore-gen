package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

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

// tcgdexClient bounds every tcgdex call: without a deadline a dead server
// holds the build for the platform's dial timeout, and the nightly runner
// spent three nights doing exactly that against a stale DNS record.
var tcgdexClient = &http.Client{Timeout: 30 * time.Second}

// loadTcgdex reads a raw GraphQL response envelope from a file when a path
// is given, or POSTs the query to the live endpoint. The request identifies
// this builder the way tcgdex's own SDKs identify themselves - their one
// header is the user agent - and a network error or server-side failure is
// retried with backoff, because the API sits behind a single host whose
// blips would otherwise cost the whole nightly.
func loadTcgdex(path, query string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt, wait := 0, 2*time.Second; attempt < 4; attempt, wait = attempt+1, wait*2 {
		if attempt > 0 {
			log.Printf("tcgdex: %v; retrying in %v", lastErr, wait)
			time.Sleep(wait)
		}
		req, err := http.NewRequest(http.MethodPost, tcgdexGraphQLURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "datastore-gen/1.0 (+https://github.com/mtgban/datastore-gen)")
		resp, err := tcgdexClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: HTTP %d", tcgdexGraphQLURL, resp.StatusCode)
			// A client-side status will not change on a retry.
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return nil, lastErr
			}
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

// cachedFetch puts a last-good cache in front of a live fetch. The fetcher
// is asked first and its answer refreshes the cache, so the cache is always
// the newest response that ever arrived; when the fetch fails the cached
// response stands in, dated out loud, rather than the annotation being
// lost. Annotation a day old is strictly better than none, and the
// baseline the build is compared against does not move on a cached run
// unless the build still grew.
//
// Both upstreams this build annotates from need it, for the same reason
// from opposite directions: tcgdex sits on one host, where a stale DNS
// record once kept the nightly dialing a dead server for three days, and
// pokemontcg.io answers 5xx often enough that a run can lose it outright.
//
// The cache only spans runs if the directory does. On a fresh CI workspace
// it is empty every time, so the file has to be carried in and out for this
// to be worth anything there; locally the directory simply persists.
func cachedFetch(source, cacheDir, cacheName string, fetch func() ([]byte, error), check func([]byte) error) ([]byte, error) {
	data, err := fetch()
	// A 200 is not an answer. GraphQL reports a failed query as a 200
	// carrying errors, and a proxy can serve an HTML page as one; written
	// to the cache unread, either poisons the fallback for every run until
	// a live fetch succeeds. The answer is decoded before it is kept.
	if err == nil {
		if cerr := check(data); cerr != nil {
			err = fmt.Errorf("%s answered, but not with a response this build can read: %w", source, cerr)
		}
	}
	if err == nil {
		if cacheDir != "" {
			if werr := os.WriteFile(filepath.Join(cacheDir, cacheName), data, 0o644); werr != nil {
				log.Printf("%s cache: %v (the build continues on the live answer)", source, werr)
			}
		}
		return data, nil
	}
	if cacheDir == "" {
		return nil, err
	}
	cached := filepath.Join(cacheDir, cacheName)
	info, serr := os.Stat(cached)
	if serr != nil {
		return nil, err
	}
	blob, rerr := os.ReadFile(cached)
	if rerr != nil {
		return nil, err
	}
	if cerr := check(blob); cerr != nil {
		return nil, fmt.Errorf("%v; and the cached response does not read either: %w", err, cerr)
	}
	log.Printf("%s unreachable (%v); using the cached response from %s",
		source, err, info.ModTime().UTC().Format("2006-01-02 15:04"))
	return blob, nil
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

// loadTcgdexCached is loadTcgdex behind that cache. An explicit response
// file bypasses everything, as it always has.
func loadTcgdexCached(path, cacheDir, cacheName, query string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	return cachedFetch("tcgdex", cacheDir, cacheName, func() ([]byte, error) {
		return loadTcgdex("", query)
	}, readsAsEnvelope)
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
