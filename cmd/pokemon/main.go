// Command pokemon builds the Pokemon datastore file consumed by go-mtgban's
// mtgmatcher loader, from the TCGplayer catalog dump for category 3, with
// annotation from the tcgdex GraphQL API.
//
// Identity is the catalog's, one entry per product and sku printing:
// TCGplayer prices Holofoil, Reverse Holofoil, 1st Edition and Unlimited as
// separate sku printings of one product, so each printing is its own entry
// with its own id, priced by construction — the finishes-as-flags flattening
// the Yu-Gi-Oh and Flesh and Blood datastores use would fold those price
// points onto one id. The id's finish suffix derives from the printing name
// alone, never from which sibling printings exist, so an id cannot churn
// when TCGplayer later adds a printing to a product.
//
// The name qualifiers — trailing parentheticals and brackets both — are told
// apart per (group, number) bucket, the One Piece election: a qualifier every
// product of the bucket carries is part of the card's name (the Unown
// letters), one that merely restates the collector number or the product's
// own Rarity is dropped, and whatever remains is the variant label the
// matcher narrows on. A drop is taken back when it would collapse two
// products of a bucket into the same (name, variant, rarity) — the "(Holo)"
// versus "(Non-Holo)" pairs must stay distinguishable. The collector number
// many names wear as a dash suffix is stripped only when it restates the
// Number field; a number-like tail that disagrees is warned about and kept,
// because the typo could be in either field.
//
// Every product the catalog types as a card becomes an entry, and validate
// refuses a build that left one out: a shape nobody has seen yet stops the
// publish instead of vanishing from the datastore. Unnumbered singles —
// basic energies, World Championship deck cards — are real cards and stay,
// with the product id alone as their id base. The few Japanese-exclusive
// singles stay too: identity is the catalog's, and the catalog types and
// prices them through English skus like any other card. The digital code
// cards (rarity "Code Card") stay on the card side rather than the sealed
// one: TCGplayer prices them through the five graded-condition skus every
// single carries, where a sealed product carries the one Unopened sku, so
// the card side is the only side that prices them at all.
//
// Sets are the catalog groups. Group abbreviations repeat freely in this
// category ("PR" 21 times), so codes are claimed in group-id order: the first
// group to claim an abbreviation keeps it bare, a later one carries its own
// group id as a suffix, and a blank abbreviation is minted from the group id.
// A set code so decided depends on the groups that came before it and never
// on the ones that come after, so an existing set keeps its code the day
// TCGplayer files a new group under an abbreviation it already uses. The
// catalog stamps the request time on groups it has no release date for, so
// only a midnight publishedOn is trusted and the joined tcgdex set fills the
// rest.
//
// tcgdex also holds cards the catalog has no product for, whole sets of them
// where TCGplayer files no group at all — the trainer kits, the McDonald's
// promos. Those are minted here, so the datastore is the sum of both
// sources rather than the catalog alone: a card the game prints is a card
// that exists, and leaving it out leaves every listing of it unresolvable.
// A minted entry names no product because there is none, and the loader
// groups an entry without a product id by its own id with the finish
// suffix stripped, which is how these are built. Its set is the group's
// where one joined and tcgdex's own id, name and release date where none
// did. Each variant tcgdex flags becomes its own entry, as a product's sku
// printings do. Some of these cards have no art upstream at all, so a
// minted entry may carry no image where an entry naming a product always
// does; the count is logged.
//
// tcgdex is annotation only, never identity: sets join by normalized name —
// retried with the short-code or EX-era prefix stripped, and an alias table
// for the promo sets tcgdex files as "Black Star Promos" — cards by localId
// against the number's numerator, and a joined card carries the tcgdex id
// and its clean image while an ambiguous or missing join carries nothing.
// The digital Pocket sets (serie "tcgp") are dropped before joining.
//
// Sealed is everything the catalog does not type as singles, one entry per
// product, so a product type TCGplayer invents next lands where it is
// noticed.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/mtgban/go-cardmarket"
	"github.com/mtgban/go-tcgplayer"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	pokemonCategory = 3

	// codeCardRarity marks the digital code cards, counted apart because
	// they are the one population here that is not a playable card.
	codeCardRarity = "Code Card"

	tcgdexGraphQLURL = "https://api.tcgdex.net/v2/graphql"

	// tcgpSerie is tcgdex's digital Pocket serie, dropped before joining.
	tcgpSerie = "tcgp"

	tcgdexSetsQuery  = "{ sets { id name releaseDate symbol serie { id } } }"
	tcgdexCardsQuery = "{ cards { id localId name rarity category image variants { normal reverse holo firstEdition } set { id } } }"
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; the only other type in this category
// is Sealed Products.
var tcgSingles = tcgplayer.SinglesProductTypes(pokemonCategory)

// finishSuffix maps each sku printing name to the suffix its entry's id
// carries; Normal is the bare id. Any other printing name is a hard failure,
// because a suffix invented on the fly would not be a stable identity.
var finishSuffix = map[string]string{
	"Normal":               "",
	"Holofoil":             "_holo",
	"Reverse Holofoil":     "_reverse",
	"1st Edition":          "_1e",
	"1st Edition Holofoil": "_1eholo",
	"Unlimited":            "_unl",
	"Unlimited Holofoil":   "_unlholo",
}

// hasDate reports whether the group's publishedOn is a real date: the
// catalog stamps the request time on groups it has no date for, so a genuine
// value is always a bare midnight timestamp.
func hasDate(g tcgplayer.Group) bool {
	return strings.HasSuffix(g.PublishedOn, "T00:00:00")
}

// tcgplayer.CatalogDump is the dump tcgdumper (github.com/mtgban/go-tcgplayer) writes
// for a category, published next to the datastore it describes.

// printingNames maps each product to the distinct printing names its English
// skus carry, in the order the catalog displays them; a printing it does not list for a
// product is one that does not exist.
func printingNames(c *tcgplayer.CatalogDump) map[int][]string {
	name := map[int]string{}
	for _, p := range c.Printings {
		name[p.PrintingID] = p.Name
	}

	// The order TCGplayer displays a category's printings in, which is the
	// catalog's to decide: a list written here would be a second opinion
	// about somebody else's data. Two printings can share a displayOrder -
	// Flesh and Blood has three at 2 - so the name settles a tie and the
	// order stays fixed for unchanged data.
	rank := map[string]int{}
	for _, p := range c.Printings {
		rank[p.Name] = p.DisplayOrder
	}

	out := map[int][]string{}
	for _, product := range c.Products {
		var names []string
		for _, sku := range product.Skus {
			if sku.LanguageID != 1 {
				continue
			}
			n := name[sku.PrintingID]
			if n == "" || sliceContains(names, n) {
				continue
			}
			names = append(names, n)
		}
		sort.SliceStable(names, func(i, j int) bool {
			if ri, rj := rank[names[i]], rank[names[j]]; ri != rj {
				return ri < rj
			}
			return names[i] < names[j]
		})
		out[product.ProductID] = names
	}
	return out
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

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

// tcgdexCard is the slice of a tcgdex card this build reads.
type tcgdexCard struct {
	ID       string `json:"id"`
	LocalID  string `json:"localId"`
	Name     string `json:"name"`
	Rarity   string `json:"rarity"`
	Category string `json:"category"`
	Image    string `json:"image"`
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
func cachedFetch(source, cacheDir, cacheName string, fetch func() ([]byte, error)) ([]byte, error) {
	data, err := fetch()
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
	log.Printf("%s unreachable (%v); using the cached response from %s",
		source, err, info.ModTime().UTC().Format("2006-01-02 15:04"))
	return blob, nil
}

// loadTcgdexCached is loadTcgdex behind that cache. An explicit response
// file bypasses everything, as it always has.
func loadTcgdexCached(path, cacheDir, cacheName, query string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	return cachedFetch("tcgdex", cacheDir, cacheName, func() ([]byte, error) {
		return loadTcgdex("", query)
	})
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

// imageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func imageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}

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
var setAliases = map[string]string{
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
}

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

// idBase mints the id stem an entry's finish suffix hangs off: the sanitized
// collector number and the product id, or the product id alone for the
// unnumbered singles.
// numberOf spells a collector number the way a query can carry it. A search
// is split on whitespace before a filter sees it, so the two halves of a
// double-faced number have to stay one token: "WTR040 // WTR039" is
// "WTR040//WTR039", and "PW 1" is "PW1". The separators are already there;
// only the spaces around them go.
func numberOf(number string) string {
	return strings.Join(strings.Fields(number), "")
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
		}
		seen[code][total] = true
	}
	out := map[string]string{}
	for code, totals := range seen {
		if len(totals) != 1 {
			continue
		}
		for total := range totals {
			out[code] = total
		}
	}
	return out
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

// componentEq compares one side of a collector number case-insensitively
// and zero-padding-insensitively: the Number field says "004/102" where the
// name says "4/102".
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

var parenTailRe = regexp.MustCompile(`\s*\(([^)]*)\)$`)
var bracketTailRe = regexp.MustCompile(`\s*\[([^\]]*)\]$`)

// numberLikeRe matches the shapes collector numbers take in this catalog —
// "8/102", "SWSH001", "TG17/TG30", "104a/102" — so a dash tail that looks
// like a number but disagrees with the Number field can be warned about.
// Bare digit runs longer than three are years ("Torchic - 2004"), not
// numbers.
var numberLikeRe = regexp.MustCompile(`^(?:[A-Za-z]{1,6}\d{1,4}[a-z]?|\d{1,3}[a-z]?)(?:/(?:[A-Za-z]{1,6}\d{1,4}[a-z]?|\d{1,3}[a-z]?))?$`)

// qual is one name qualifier with the delimiter style it wore, kept so an
// elected name part is restored in its own brackets.
// promoGroups reports which catalog groups hand out promotional printings.
// Two things say so and they cover different ground: TCGplayer names most of
// them, and the league, championship and blister groups hand out their cards
// without saying so in the name, which the products' own rarity records. The
// rarity test asks for every card product in the group, not most: a set that
// merely holds some promos among its pack cards is not a promotional set,
// and reading it as one would make a promo of everything beside them.
func promoGroups(catalog tcgplayer.CatalogDump) map[int]bool {
	cards := map[int]int{}
	promos := map[int]int{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		cards[product.GroupID]++
		if strings.Contains(strings.ToLower(product.Extended("Rarity")), "promo") {
			promos[product.GroupID]++
		}
	}
	out := map[int]bool{}
	for _, group := range catalog.Groups {
		if strings.Contains(strings.ToLower(group.Name), "promo") {
			out[group.GroupID] = true
			continue
		}
		if n := cards[group.GroupID]; n > 0 && promos[group.GroupID] == n {
			out[group.GroupID] = true
		}
	}
	return out
}

// promoTypesOf folds a printing's surviving qualifiers to the spelling the
// matcher declares tags in.
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

// formeRe matches a label naming which shape a Pokemon is in.
var formeRe = regexp.MustCompile(`(?i)^(?:.+ forme|form [a-z])$`)

// deckPlaceRe splits a qualifier into a name and the number behind it.
var deckPlaceRe = regexp.MustCompile(`^(.+?)\s+([0-9]{1,3})$`)

// variantOnlyQuals name a label that tells two printings apart without
// saying anything promoted either, and which no rule here can recognise on
// its own. Trading Card Game Classic ships three decks - Blastoise,
// Charizard and Venusaur - and every card is in all three, so CLB, CLC and
// CLV say which deck a copy came from and nothing more. Thirteen cards
// each, and the marker is the only thing between the three copies, so it
// stays a variant rather than leaving.
var variantOnlyQuals = map[string]bool{
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
	// leader who awards it. Which badge, not what promoted it.
	"giovanni": true,
}

// namesASet reports whether a label names one of the sets this datastore
// carries, reading past the era the catalog writes in front where our own
// name does not carry it: "DP Legends Awakened" against "Legends Awakened",
// "HGSS Undaunted" against "Undaunted", beside an "EX Hidden Legends" that
// matches whole. The head has to be short enough to be an era and there has
// to be a name behind it, so nothing reads the first word of a two-word set
// as a prefix of the second.
func namesASet(label string, setNames map[string]bool) bool {
	if setNames[mtgmatcherNormalize(label)] {
		return true
	}
	head, rest, found := strings.Cut(label, " ")
	if !found || len(head) > 4 || !strings.Contains(rest, " ") && len(rest) < 4 {
		return false
	}
	return setNames[mtgmatcherNormalize(rest)]
}

func promoTypesOf(s *single, pokemon, setNames map[string]bool, onShelf bool) []string {
	// A code card is a redemption slip, and its label names what the code
	// unlocks or the box it came in: "Code Card - Steam Siege Collectible
	// Pin 3 Pack Blister [Shiny Mega Gardevoir]", "[Ballonea Gym]",
	// "[Chespin Box]". Nothing promoted the slip. Seventy labels were in the
	// promo types for no other reason, none of them shared with a card
	// anybody plays, and each still tells its own slip from the others as a
	// variant.
	if s.product.Extended("Rarity") == codeCardRarity {
		return nil
	}
	out := make([]string, 0, len(s.quals))
	for _, q := range s.quals {
		if variantOnlyQuals[strings.ToLower(q.text)] {
			continue
		}
		if onShelf && namesASet(q.text, setNames) {
			continue
		}
		// Which shape the Pokemon is in, which no card was promoted for:
		// Deoxys in its Normal, Attack, Defense and Speed Formes at four
		// numbers of its own, Shaymin Lv.X in its Land and Sky, and the two
		// M Charizard EX that say Form X and Form Y where the four others
		// say a bare X and Y this already leaves out.
		if formeRe.MatchString(q.text) {
			continue
		}
		if pokemon[mtgmatcherNormalize(q.text)] || bareNumberingRe.MatchString(q.text) {
			continue
		}
		// A Pokemon and a number is a deck and a place in it. Battle
		// Academy names its decks after Pokemon and numbers the cards
		// within each - "Armarouge ex (Armarouge 60)" is card 60 of the
		// Armarouge deck, and the card's own number is 105.
		if m := deckPlaceRe.FindStringSubmatch(q.text); m != nil && pokemon[mtgmatcherNormalize(m[1])] {
			continue
		}
		// And a list of them is still them. A blister naming what is inside
		// it - "Glaceon/Vaporeon/Sylveon/Espeon", "Articuno, Zapdos, &
		// Moltres", "Mew & Mewtwo" - names 25 labels no two cards share,
		// and every one is a variant for the same reason a single Pokemon
		// is. Every part has to be a Pokemon before any of it is taken.
		if allPokemon(q.text, pokemon) {
			continue
		}
		text := q.text
		if m := stampPlaceRe.FindStringSubmatch(text); m != nil {
			text = m[1]
		}
		out = append(out, strings.ToLower(text))
	}
	return out
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
// reverse holo wears; "Cyrus", "Ghetsis" and "Professor Rowan" are card
// names and they say which Supporter. tcgdex files all six as Trainers, so
// none of them is touched.
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

type qual struct {
	text    string
	bracket bool
}

func (q qual) String() string {
	if q.bracket {
		return "[" + q.text + "]"
	}
	return "(" + q.text + ")"
}

// single is one card product, its name split into the base name, the
// qualifiers still up for election, and the ones dropped for restating the
// number or the rarity — kept around so the collision guard can take a drop
// back.
type single struct {
	product  tcgplayer.Product
	number   string
	baseName string
	quals    []qual
	dropped  []qual
}

// foldQualKey is what two spellings of one qualifier have in common: no
// case, no punctuation, and no plural on the end. One "s" is dropped rather
// than every trailing one, and only from a word long enough to still be a
// word without it, so "boss" does not become "bos".
// Case is folded away with everything else except on "ex", where it is the
// whole meaning: "Charizard ex" is a Scarlet & Violet card and "Charizard
// EX" an XY one, two mechanics a decade apart that a case-blind key would
// read as one spelling of the other.
func foldQualKey(qual string) string {
	var key strings.Builder
	for _, field := range strings.Fields(qual) {
		if strings.EqualFold(field, "ex") {
			key.WriteString(field)
			continue
		}
		key.WriteString(mtgmatcherNormalize(field))
	}
	out := foldPunctuationOnly(key.String())
	if out == "" {
		out = strings.ToLower(strings.TrimSpace(qual))
	}
	if len(out) > 3 && strings.HasSuffix(out, "s") {
		out = out[:len(out)-1]
	}
	return out
}

// qualNameKey identifies one card's name paired with one qualifier, folded
// so the catalog's punctuation and casing never decide.
func qualNameKey(base, qual string) string {
	folded := mtgmatcherNormalize(qual)
	if folded == "" {
		folded = strings.ToLower(strings.TrimSpace(qual))
	}
	return mtgmatcherNormalize(base) + "|" + folded
}

// foldPunctuationOnly keeps a qualifier that is nothing but punctuation
// telling itself apart. Unseen Forces names two Unown "!" and "?", and a
// key that drops every non-alphanumeric makes those the same qualifier -
// which spelled both of them "?" until this.
func foldPunctuationOnly(qual string) string {
	return mtgmatcherNormalize(qual)
}

// dexNameQuals collects the parentheticals tcgdex prints as part of a card's
// own name. Thirteen of its 23,546 names carry one and they are all the same
// thing, the character a Supporter names: "Boss's Orders (Giovanni)",
// "Professor's Research (Professor Oak)", "Thought Wave Machine (Rocket's
// Secret Machine)". Everything else the catalog hangs off a name belongs to
// the printing.
//
// This used to be an election: a qualifier every product of a (group,
// number) bucket carried was taken to be part of the card, on the reasoning
// that what tells none of them apart cannot be telling them apart. The
// reasoning does not survive contact with upstream. Of the 549 entries whose
// names the election had spelled with a parenthetical, tcgdex carries the
// parenthetical on none, the bare name on 500, and a name differing only in
// how an EX is hyphenated on the other 49 - so every name it ever made was
// a name no card prints. "Prerelease" was 302 of them and "Team Plasma" 196,
// neither of which appears in any of tcgdex's 23,546 names.
//
// Upstream is asked rather than a vocabulary kept because a vocabulary has
// to be guessed at, and which parentheticals belong to a name is a fact
// somebody already recorded. tcgdex's own spelling is read raw here: putting
// it through catalogSpelling would turn every Delta Species mark into a
// parenthetical and teach this the opposite of what it is for.
// The value is how upstream writes the qualifier, not how the catalog does.
// Both spell the same thing differently - the catalog brackets the Unown
// letter, "Unown [A]" and "Unown (J)", where tcgdex writes "Unown A" - and
// keeping the catalog's delimiter left one card family spelled three ways.
func dexNameQuals(cards []tcgdexCard) map[string]string {
	names := map[string]bool{}
	for i := range cards {
		names[mtgmatcherNormalize(cards[i].Name)] = true
	}
	out := map[string]string{}
	for i := range cards {
		name := cards[i].Name
		if m := parenTailRe.FindStringSubmatch(name); m != nil {
			if base := strings.TrimSpace(strings.TrimSuffix(name, m[0])); base != "" {
				qual := strings.TrimSpace(m[1])
				out[qualNameKey(base, qual)] = "(" + qual + ")"
			}
			continue
		}
		// Upstream brackets some of them: the Unown letters it does not
		// write bare, and "Ancient Technical Machine [Rock]" beside its Ice
		// and Steel. Thirty names carry one, and reading only the
		// parenthesised ones filed "[Rock]" as a promo type on a card whose
		// two siblings differ from it by nothing else.
		if m := bracketTailRe.FindStringSubmatch(name); m != nil {
			if base := strings.TrimSpace(strings.TrimSuffix(name, m[0])); base != "" {
				qual := strings.TrimSpace(m[1])
				out[qualNameKey(base, qual)] = "[" + qual + "]"
			}
			continue
		}
		// Upstream also writes a name qualifier with nothing around it,
		// which is how the Unown letters are spelled: "Unown A", "Unown !",
		// "Unown ?". The catalog parenthesises them - "Unown (!)" - and
		// they would otherwise leave as promo types called "!" and "?".
		//
		// The last word is only taken as a qualifier when what precedes it
		// is a card name in its own right, which is what keeps "Dark
		// Tyranitar" from being read as a Tyranitar qualified by "Dark".
		// Read with the marks spelled the way the catalog spells them, so
		// "Nidoran \u2640" is indexed as the "Nidoran (F)" the catalog
		// writes and the gender stays part of the name instead of leaving
		// as a promo type called "f". Only the marks: catalogSpelling would
		// also turn every Delta Species into a parenthetical and teach this
		// the opposite of what it is for.
		name = strings.Join(strings.Fields(tcgdexSymbols.Replace(name)), " ")
		idx := strings.LastIndex(name, " ")
		if idx <= 0 {
			continue
		}
		base, tail := name[:idx], strings.TrimSpace(name[idx+1:])
		if tail == "" || !names[mtgmatcherNormalize(base)] {
			continue
		}
		out[qualNameKey(base, tail)] = tail
	}
	return out
}

// cosmosSpelling matches the holo pattern the catalog names five ways.
var cosmosSpelling = regexp.MustCompile(`(?i)\bCosmos?[ ]+(?:Holofoil|Holo|Foil)\b`)

// oneCosmos spells that pattern the one way. The catalog files it as
// "Cosmos Holo" 170 times, "Cosmo Holo" 17, "Cosmos Foil" 7, "Cosmo Foil"
// 6 and "Cosmos Holofoil" 4: one printing wearing five labels, so a query
// naming any of them misses the cards filed under the other four.
//
// Only the pattern's own words are rewritten. The qualifiers around it are
// what tell real printings apart - a "Reverse Cosmos Holo" and a "Cosmos
// Holo Costco Exclusive" are not the plain one - and they are left as they
// are. No card carries two of the five spellings at one identity, so this
// folds no two printings into one. The Pokemon named Cosmog is not
// matched: the pattern needs a treatment word after the stem.
func oneCosmos(qualifier string) string {
	return cosmosSpelling.ReplaceAllString(qualifier, "Cosmos Holo")
}

// gameStopSpelling matches the retailer the catalog names eight ways over
// twenty-four products: "Gamestop Exclusive" 12 times, "GameStop Exclusive"
// 4, "Gamestop Promo" 3, and one each of "GameStop Promo", "GameStop Metal
// Card", "Gamestop" inside "Non-Holo Gamestop Exclusive", "GameStop" and
// "Game Stop".
var gameStopSpelling = regexp.MustCompile(`(?i)\bGame\s*Stop\b`)

// oneGameStop writes the retailer's name the way the retailer writes it.
// Frequency says "Gamestop" and the election below would duly elect it, but
// a majority of a typo is still a typo. Rewriting the word rather than the
// labels is what keeps the family together: the bare label, the two it
// leads, and the one it sits in the middle of all read alike, and a label
// the catalog has not invented yet will too.
func oneGameStop(qualifier string) string {
	return gameStopSpelling.ReplaceAllString(qualifier, "GameStop")
}

// trimQualJoiners takes off a character the catalog uses to join two things
// and left dangling at the end of one. It closes two Ralts numbers with a
// stray slash, "(060/189/)" and "(067/195/)", which is enough to stop the
// number being read as a number: the qualifier survived the drop, the split
// took "060/189" off the front, and what it left behind became a promo type
// called "/". No qualifier the catalog means ends in one of these.
func trimQualJoiners(qualifier string) string {
	return strings.TrimRight(strings.TrimSpace(qualifier), " /,-")
}

// spelledNumbers writes a placing the way the rest of them are written.
// The catalog spells two of them out - "2014 Top Sixteen" beside "Top 16",
// "Top Thirty-Two" beside "Top 32" - and a word and a numeral fold to no
// common key however they are normalised.
var spelledNumbers = strings.NewReplacer(
	"Thirty-Two", "32", "Thirty Two", "32", "Sixteen", "16",
)

// worldsLabelRe write the championship the one way. The catalog names it
// four: "Worlds 07" through "Worlds 13", "2004 World Championships" with
// the year in front, "World Championship 2025" without its plural, and
// "World Championships 2017" which is what the other three become.
var (
	worldsShortRe    = regexp.MustCompile(`^Worlds\s+(\d{2})$`)
	worldsLeadYearRe = regexp.MustCompile(`^((?:19|20)\d{2})\s+World Championships?$`)
	worldsSingularRe = regexp.MustCompile(`^World Championship\s+((?:19|20)\d{2})$`)
)

// blackStarRe matches the promo shelf the catalog names singular and
// plural in one breath: "SM Black Star Promo" 21 times beside "XY Black
// Star Promos" 11, and each spelling elected its own because the election
// compares one label at a time. The shelf is "Black Star Promos".
var blackStarRe = regexp.MustCompile(`(?i)black star promos?$`)

func oneBlackStar(qualifier string) string {
	return blackStarRe.ReplaceAllString(qualifier, "Black Star Promos")
}

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

// respellQual writes a qualifier the one way this datastore spells it,
// where the catalog spells it several.
func respellQual(qualifier string) string {
	q := trimQualJoiners(qualifier)
	return oneBlackStar(oneWorldsLabel(spelledNumbers.Replace(oneGameStop(oneCosmos(q)))))
}

// peelQuals peels the trailing parenthetical and bracket qualifiers off a
// name, outermost last, preserving their order and dropping a repeat of a
// qualifier the product already carries.
func peelQuals(name string) (string, []qual) {
	var quals []qual
	for {
		var q qual
		if m := parenTailRe.FindStringSubmatch(name); m != nil {
			q = qual{text: respellQual(strings.TrimSpace(m[1]))}
			name = strings.TrimSuffix(name, m[0])
		} else if m := bracketTailRe.FindStringSubmatch(name); m != nil {
			q = qual{text: respellQual(strings.TrimSpace(m[1])), bracket: true}
			name = strings.TrimSuffix(name, m[0])
		} else {
			break
		}
		if q.text == "" {
			continue
		}
		duplicate := false
		for _, seen := range quals {
			if seen.text == q.text {
				duplicate = true
			}
		}
		if !duplicate {
			quals = append([]qual{q}, quals...)
		}
	}
	return strings.TrimSpace(name), quals
}

// decompose splits a product name into base name and qualifiers, strips the
// dash-hung collector number, and applies the pre-election drops. The dash
// number sits between the name and the trailing qualifiers, so the
// qualifiers peel first and the tail check runs on what remains.
// rawNames repair a product name the catalog wrote in a shape nothing can
// read, keyed by the product id it never reuses. Only four names in 32,675
// close a bracket they never opened and three of those are sealed, so this
// is a repair rather than a rule: the peeler should not be taught to guess
// where a missing parenthesis went.
// droppedQuals name a label that survives the peel and says nothing worth
// carrying. "English" is the language every card in this datastore is
// printed in, so it distinguishes a Pikachu from nothing; the six other
// languages beside it in the same set do distinguish theirs and stay.
var droppedQuals = map[string]bool{
	"english": true,
}

// isBareLetter reports whether a qualifier is a single letter and nothing
// else. Where the letter belongs to the card, upstream says so and it has
// already been taken into the name - the Unown letters, Nidoran's gender.
// What is left is a letter the catalog wrote and the number already says:
// "M Charizard EX (X)" is numbered 69 where its Y is numbered 13, and a
// promo type spelled "x" helps no one read that.
// setOfRe matches the count a multi-card product carries in its name,
// written either way: "Zacian V-UNION [Set of 4]" and "Code Card - Mini
// Portfolio [Giratina] (2-Pack)".
var setOfRe = regexp.MustCompile(`(?i)^(?:set of \d+|\d+-pack)$`)

func isBareLetter(qualifier string) bool {
	if len([]rune(qualifier)) != 1 {
		return false
	}
	r := []rune(strings.ToLower(qualifier))[0]
	return r >= 'a' && r <= 'z'
}

// qualSplits name a qualifier the catalog wrote as one label and every
// other card writes as two. "Finneon - SWSH240 (Prerelease Staff)" is the
// only card of 298 staff printings to run the two words together, and the
// three Non-Holos are a surface and a distribution said in one breath.
var qualSplits = map[string][]string{
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
}

var qualSpellings = map[string]string{
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
	"gamestop metal card":         "GameStop Exclusive",
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
}

var rawNames = map[int]string{
	// The catalog never closes the parenthesis: "Chesnaught - XY68
	// (Prerelease [Staff]". Both qualifiers are real and neither is
	// reachable while the name is unbalanced.
	108598: "Chesnaught - XY68 (Prerelease) [Staff]",
	// A stray letter after the closing parenthesis, which stops the peel
	// dead: "Jet Energy - 2023 (Gabriel Fernandez)a".
	541801: "Jet Energy - 2023 (Gabriel Fernandez)",
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

func decompose(p tcgplayer.Product, num, year string) (single, int) {
	name := p.Name
	if repaired, hand := rawNames[p.ProductID]; hand {
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
		if !restatesNumber(tail, num) {
			if skip == 3 && numberLikeRe.MatchString(tail) {
				log.Printf("dash number: %q keeps tail %q, Number is %q", p.Name, tail, num)
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
		parts, hand := qualSplits[strings.ToLower(q.text)]
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
var renumberedSets = map[string]bool{
	"cel25cc": true, // Celebrations: Classic Collection
}

// shadowShare is how much of a tcgdex set has to be the catalog's already
// for the set to be the catalog's under another name. The measured sets sit
// at 67% and above or at 33% and below, so anything in the gap separates
// them; half is the middle of it and nothing sits near either side.
const shadowShare = 0.5

// pokemontcgSetsURL is pokemontcg.io's set list, read for the one field
// tcgdex leaves empty on 49 of its 218 sets: the symbol. Every one of its
// 174 sets carries one.
const pokemontcgSetsURL = "https://api.pokemontcg.io/v2/sets?pageSize=250"

// pokemontcgSet is the slice of a pokemontcg.io set this build reads.
type pokemontcgSet struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PtcgoCode   string `json:"ptcgoCode"`
	ReleaseDate string `json:"releaseDate"`
	Images      struct {
		Symbol string `json:"symbol"`
	} `json:"images"`
}

// pokemontcgClient bounds every pokemontcg.io call, the way tcgdexClient
// bounds tcgdex's: without a deadline a host that has stopped answering
// holds the build for the platform's dial timeout.
var pokemontcgClient = &http.Client{Timeout: 30 * time.Second}

// fetchPokemontcgSets asks the live API, retried the way tcgdex is. One
// attempt was losing the symbols to blips that clear on their own: three
// consecutive requests one afternoon answered 500, then 502, then 200. A
// status the server will give again on a retry is not retried.
func fetchPokemontcgSets() ([]byte, error) {
	var lastErr error
	for attempt, wait := 0, 2*time.Second; attempt < 4; attempt, wait = attempt+1, wait*2 {
		if attempt > 0 {
			log.Printf("pokemontcg.io: %v; retrying in %v", lastErr, wait)
			time.Sleep(wait)
		}
		req, err := http.NewRequest(http.MethodGet, pokemontcgSetsURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "datastore-gen/1.0 (+https://github.com/mtgban/datastore-gen)")
		resp, err := pokemontcgClient.Do(req)
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
			lastErr = fmt.Errorf("%s: HTTP %d", pokemontcgSetsURL, resp.StatusCode)
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

// loadPokemontcgSets reads the set list from a path, or from the live API
// behind the last-good cache. A failure is reported and not fatal: this
// fills the symbol of 20 sets the other 255 do not need, and no build is
// worth losing over it. What the cache buys is that those 20 keep the
// symbol they had last time instead of silently going blank.
func loadPokemontcgSets(path, cacheDir string) ([]pokemontcgSet, error) {
	var data []byte
	var err error
	if path != "" {
		data, err = os.ReadFile(path)
	} else {
		data, err = cachedFetch("pokemontcg.io", cacheDir, "pokemontcg-sets.json", fetchPokemontcgSets)
	}
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []pokemontcgSet `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

// mtgmatcherNormalize reduces a set name to its letters and digits, so the
// two sources' punctuation and casing do not part names that are the same.
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

// pokemontcgDate spells a pokemontcg.io release date the way this datastore
// spells one: it writes "1999/01/09" where every date here is "1999-01-09".
func pokemontcgDate(date string) string {
	return strings.ReplaceAll(date, "/", "-")
}

// nonCodeRe matches the runs a set code cannot carry.
var nonCodeRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// handNames correct the names the catalog misspells, keyed by the product
// id, which the catalog never reuses. Each one is the catalog contradicting
// its own spelling elsewhere, which is what makes it a typo rather than a
// naming convention: 59 products spell the Pokemon "Exeggutor" and one
// spells it "Exeggcutor", 27 spell "Drowzee" and one "Drowsee", 28 spell
// "Technical Machine" and one abbreviates it to "Technical Mach.".
//
// The bar is that high because a name spelled differently across printings
// is usually the printings differing, not the catalog erring. Impostor
// Professor Oak is the case that proves it: Wizards printed the Base Set
// card "Impostor" and the Base Set 2 and Celebrations reprints "Imposter",
// and the catalog has all three right. Only the Base Set product is wrong,
// and only because the catalog's own Shadowless printing of that same card
// spells it the other way. A rule that had corrected all the "Imposter"
// products alike would have introduced two errors to fix one.
//
// A convention is not a typo either, and stays: the special energies
// transcribe the symbol printed on the card as a letter ("Heat R Energy",
// "Speed L Energy"), which the catalog does consistently.
func handName(productID int, name string) string {
	if corrected, hand := handNames[productID]; hand {
		return corrected
	}
	return name
}

var handNames = map[int]string{
	// Base Set #073/102. The catalog's own Shadowless printing of this
	// card (107070) spells it Impostor, as do tcgdex and pokemontcg.io.
	// The Base Set 2 and Celebrations reprints really are "Imposter" and
	// are left alone.
	86271: "Impostor Professor Oak",
	// The Pokemon is Exeggutor, as this catalog's other 59 products say.
	84593: "Dark Exeggutor",
	// The Pokemon is Drowzee, as this catalog's other 27 products say.
	84973: "Drowzee",
	// Truncated rather than misspelled, but a name no query carries.
	89808: "Team Galactic's Invention G-107 Technical Machine G",
}

// reportStaleHandTables says when a hand-maintained row has stopped doing
// anything. Both tables here correct a source rather than add to it, so a
// row outlives its reason silently: the catalog fixes a spelling, or gains
// a real date, and the row goes on being consulted and changing nothing.
// Nobody would notice, and the next person to read the table would take
// every row in it as still true.
//
// Nothing is dropped automatically. A row the catalog agrees with today
// may disagree again tomorrow - the catalog has re-broken a name before -
// and a row whose product has vanished may be a product withdrawn for a
// week. This reports; a person decides.
func reportStaleHandTables(catalog tcgplayer.CatalogDump, dated map[int]bool) {
	names := map[int]string{}
	for _, product := range catalog.Products {
		names[product.ProductID] = product.Name
	}
	for productID, corrected := range handNames {
		switch have, sold := names[productID]; {
		case !sold:
			log.Printf("hand names: product %d is no longer in the catalog; the row correcting it to %q does nothing",
				productID, corrected)
		case have == corrected:
			log.Printf("hand names: the catalog now spells product %d %q itself; the row is a no-op",
				productID, corrected)
		}
	}

	groups := map[int]bool{}
	for _, group := range catalog.Groups {
		groups[group.GroupID] = true
	}
	for groupID, date := range handDates {
		switch {
		case !groups[groupID]:
			log.Printf("hand dates: group %d is no longer in the catalog; the row dating it %s does nothing",
				groupID, date)
		case dated[groupID]:
			log.Printf("hand dates: group %d is dated by a source now; the row dating it %s is a no-op",
				groupID, date)
		}
	}
}

// handDates are release dates for the groups neither source can date: the
// catalog stamps its request time on them and tcgdex has no set to join.
// Each is researched, not guessed, and keyed by the group id, which the
// catalog never reuses. A group holding one promotion carries that
// promotion's release day; a drawer TCGplayer fills across eras carries
// the earliest day its contents can have existed, so it sorts where its
// oldest cards belong.
var handDates = map[int]string{
	// EX Trainer Kit released June 1, 2004 (Bulbapedia, "EX Trainer Kit").
	1543: "2004-06-01",
	// EX Trainer Kit 2 released March 1, 2006 (Bulbapedia, "EX Trainer
	// Kit 2").
	1542: "2006-03-01",
	// The 2008 Burger King Kids Meal promotion ran July 7 to August 12,
	// 2008 (Bulbapedia, "2008 Burger King promotional Pokémon toys"); the
	// group also holds the later Platinum-stamped wave.
	2175: "2008-07-07",
	// The original Pikachu World Collection, sold at Pokémon Park 2000 in
	// Sydney from September 12, 2000 (Bulbapedia, "Pikachu World
	// Collection"); the group also holds the 2010 nine-language box.
	2205: "2000-09-12",
	// The First Partner Packs opened with the Galar pack on March 5, 2021
	// and ran monthly through October.
	2776: "2021-03-05",
	// The alternate-art "a"-numbered promos begin with prints derived
	// from XY Furious Fists (55a/111), released August 13, 2014.
	1938: "2014-08-13",
	// The cosmos-holo blister exclusives begin with prints of the XY base
	// set (/146), released February 5, 2014.
	2289: "2014-02-05",
	// The professor-program cards begin with the 2004-2005 cycle, printed
	// from EX FireRed & LeafGreen, released August 30, 2004.
	2332: "2004-08-30",
	// The miscellaneous drawer's oldest cards are stamped Base Set prints
	// handed out through 1999; anchored at the game's English release.
	2374: "1999-01-09",
}

// setCodeOf reduces a catalog abbreviation to what a search query can carry.
// A set code is typed after "is:", and a query is split on whitespace before
// a filter ever sees it and on the colon that names the filter, so a code
// holding either cannot be asked for: "is:OP11 RE" reaches the filter as
// "is:OP11" and "is:crz:gg" names a filter called crz. Every run of anything
// but a letter or a digit becomes one dash, and the ends are trimmed of them.
func setCodeOf(abbreviation string) string {
	return strings.Trim(nonCodeRe.ReplaceAllString(abbreviation, "-"), "-")
}

// datastoreCounts is what a datastore holds: the two totals, and the card
// count per set. It is read off an encoded datastore - this build's own, or
// the one it is about to replace - so both sides are counted the same way
// by the same code.
type datastoreCounts struct {
	cards, sealed int
	bySet         map[string]int
}

// cardmarketMintSets names the promo shelves whose Cardmarket catalog runs
// wider than every catalog we build from, mapped to the set of ours their
// products belong to. Both are stamp programmes: a card is reprinted with a
// programme's stamp and keeps the number it had in the set it came from, so
// Cardmarket writes the number as that set's code and number ("SVI 081").
//
// TCGplayer lists 35 of Cardmarket's 85 Southeast Asia products and 57 of
// its 66 Professor Program ones, and the two catalogs barely overlap - 17 of
// our 32 Southeast Asia cards are on no Cardmarket shelf at all. tcgdex and
// pokemontcg.io carry neither set in any language, and CardTrader, which
// carries both, bridges none of the missing products to a TCGplayer id. So
// this is the only source there is for them.
var cardmarketMintSets = map[string]string{
	"Southeast Asia Promos": "SEA",
	"Professor Program":     "PPP",
}

// cardmarketSourceNumber splits the number Cardmarket writes on a stamped
// promo into the set it was reprinted from and the number it kept.
var cardmarketSourceNumber = regexp.MustCompile(`^([A-Za-z0-9-]{2,6}) *([0-9]+[a-z]?)$`)

// mintFromCardmarket adds a row for every product of those shelves that no
// catalog of ours carries. It answers how many it minted and how many it
// passed over.
//
// THE FINISH ON THESE ROWS IS A DEFAULT, NOT A FACT. Nothing publishes it:
// Cardmarket's record has no finish field, its rarity is the constant
// "Promo" on these shelves, and CardTrader's `pokemon_reverse` says whether
// a reverse variant is sellable rather than what the card is. Nor is it
// derivable from the card being stamped: measured against the 32 Southeast
// Asia cards we do carry, the source printing's own finishes predict the
// promo's on 5 of them, and the 21 whose source comes in all three finishes
// split 9 Holofoil, 9 Normal and 3 both. The rule below - holo when the
// source printing is sold in no other finish, plain otherwise - is right on
// 21 of those 32, which is the best any rule managed. Vittorio's call on
// 2026-09-07 was that a card priced with a guessed finish beats a card not
// priced at all, to be corrected case by case as any row is found wrong.
//
// A product is minted only where the printing it was stamped from is one we
// already carry, which is what keeps a mis-parsed number from inventing a
// card. That row also lends the promo its printed total.
func mintFromCardmarket(path string, cards []any) ([]any, int, int) {
	file, err := os.Open(path)
	if err != nil {
		log.Fatalln("cardmarket catalog:", err)
	}
	defer file.Close()
	catalog, err := cardmarket.LoadCatalog(file)
	if err != nil {
		log.Fatalln("cardmarket catalog:", err)
	}

	// The shelves we mint from, by the id the products name them with.
	shelves := map[int]string{}
	for id, expansion := range catalog.Data.Expansions {
		code, wanted := cardmarketMintSets[expansion.Name]
		if !wanted {
			continue
		}
		shelves[id] = code
	}
	for name := range cardmarketMintSets {
		var found bool
		for _, expansion := range catalog.Data.Expansions {
			if expansion.Name == name {
				found = true
			}
		}
		if !found {
			log.Printf("cardmarket: no shelf named %q any more; its products mint nothing", name)
		}
	}

	// What we already hold, so a product the catalog covers is passed over,
	// and what each printing was sold as, so a promo can take its total.
	held := map[string]bool{}
	sources := map[string][]map[string]any{}
	for _, entry := range cards {
		row, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		number, _ := row["number"].(string)
		code, _ := row["setCode"].(string)
		held[code+"|"+strings.ToLower(name)+"|"+unpad(number)] = true
		key := strings.ToLower(name) + "|" + unpad(number)
		sources[key] = append(sources[key], row)
	}

	ids := make([]int, 0, len(catalog.Data.Products))
	for id := range catalog.Data.Products {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	var minted, passed int
	for _, id := range ids {
		product := catalog.Data.Products[id]
		code, wanted := shelves[product.ExpansionID]
		if !wanted {
			continue
		}
		fields := cardmarketSourceNumber.FindStringSubmatch(strings.TrimSpace(product.Number))
		if fields == nil {
			passed++
			continue
		}
		name := catalogSpelling(strings.TrimSpace(product.Name))
		number := fields[2]
		if held[code+"|"+strings.ToLower(name)+"|"+unpad(number)] {
			continue
		}
		// The printing it was stamped from, which has to be one of ours.
		from := sources[strings.ToLower(name)+"|"+unpad(number)]
		if len(from) == 0 {
			passed++
			continue
		}
		total, _ := from[0]["total"].(string)
		finishes := map[string]bool{}
		for _, row := range from {
			finish, _ := row["finish"].(string)
			finishes[finish] = true
		}
		finish := "Normal"
		if len(finishes) == 1 && finishes["Holofoil"] {
			finish = "Holofoil"
		}

		// A catalog written before mkmcatalog carried the rarity says
		// nothing of it, and both shelves are promo programmes whole - the
		// marketplace calls all 85 Southeast Asia products and all 66
		// Professor Program ones "Promo" - so that is what a silent catalog
		// means here. A shelf added to the table that is not a promo
		// programme wants a catalog new enough to say so.
		rarity := product.Rarity
		if rarity == "" {
			rarity = "Promo"
		}
		entry := map[string]any{
			"id":      fmt.Sprintf("%s_mkm%d%s", sanitizeID(number+"-"+total), id, finishSuffixFor(finish)),
			"name":    name,
			"setCode": code,
			"number":  number,
			"rarity":  rarity,
			"finish":  finish,
		}
		if total != "" {
			entry["total"] = total
		}
		cards = append(cards, entry)
		held[code+"|"+strings.ToLower(name)+"|"+unpad(number)] = true
		minted++
	}
	return cards, minted, passed
}

func countDatastore(data []byte) (datastoreCounts, error) {
	var doc struct {
		Cards []struct {
			SetCode string `json:"setCode"`
		} `json:"cards"`
		Sealed []json.RawMessage `json:"sealed"`
	}
	out := datastoreCounts{bySet: map[string]int{}}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	out.cards = len(doc.Cards)
	out.sealed = len(doc.Sealed)
	for _, card := range doc.Cards {
		out.bySet[card.SetCode]++
	}
	return out, nil
}

// regression compares this build against the datastore it is about to
// replace and refuses to publish one that lost a meaningful share of it.
// The minimum card count this used to be checked against was a number
// invented once and never revisited, far below what the datastore actually
// holds, so a build could lose a third of itself and still publish. The
// previous datastore is the number that keeps itself up to date.
//
// Only shrinkage is suspicious - these datastores grow every week - and
// only three shapes of it are refused: a total that fell by more than the
// tolerance, a set that holds no card at all any more, and a set that lost
// more than half of what it held. The last two are what a whole-file count
// cannot see: one set folding onto another moves the total by a fraction
// of a percent while emptying a set completely. Every other per-set drop is
// logged rather than refused, because a product delisted here and there is
// ordinary and a build that cried wolf would be turned off.
func regression(previous, current datastoreCounts, tolerance float64) error {
	if previous.cards == 0 {
		return nil
	}
	lost := func(was, now int) bool {
		return now < was && float64(was-now)/float64(was) > tolerance
	}
	if lost(previous.cards, current.cards) {
		return fmt.Errorf("%d cards, down from %d, more than the %.1f%% a build may lose",
			current.cards, previous.cards, tolerance*100)
	}
	if lost(previous.sealed, current.sealed) {
		return fmt.Errorf("%d sealed products, down from %d, more than the %.1f%% a build may lose",
			current.sealed, previous.sealed, tolerance*100)
	}
	var vanished, collapsed, shrank []string
	for code, was := range previous.bySet {
		now := current.bySet[code]
		switch {
		case now == 0:
			vanished = append(vanished, code)
		case now*2 < was:
			collapsed = append(collapsed, fmt.Sprintf("%s %d->%d", code, was, now))
		case now < was:
			shrank = append(shrank, fmt.Sprintf("%s %d->%d", code, was, now))
		}
	}
	sort.Strings(vanished)
	sort.Strings(collapsed)
	sort.Strings(shrank)
	for _, s := range shrank {
		log.Printf("against: set %s", s)
	}
	if len(vanished) > 0 {
		return fmt.Errorf("%d sets hold no card any more: %s",
			len(vanished), strings.Join(vanished, " "))
	}
	if len(collapsed) > 0 {
		return fmt.Errorf("%d sets lost more than half of what they held: %s",
			len(collapsed), strings.Join(collapsed, " "))
	}
	return nil
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 3 (required)")
	tcgdexSets := flag.String("tcgdex-sets", "", "tcgdex sets GraphQL response file (default: query the live API)")
	tcgdexCards := flag.String("tcgdex-cards", "", "tcgdex cards GraphQL response file (default: query the live API)")
	pokemontcgSets := flag.String("pokemontcg-sets", "", "pokemontcg.io sets response file, read for the symbols tcgdex has none of (default: query the live API)")
	upstreamCache := flag.String("upstream-cache", "", "directory holding the last good tcgdex and pokemontcg.io responses, refreshed on a live fetch and read back when an API is unreachable")
	cardmarketCatalogPath := flag.String("cardmarket-catalog", "", "published Cardmarket catalog, read for the stamped promos no other catalog lists (required)")
	against := flag.String("against", "", "baseline datastore to compare against; refuses a build that lost a large share of it")
	againstTolerance := flag.Float64("against-tolerance", 0.01, "the share of its cards or sealed products a build may lose")
	baselineFit := flag.String("baseline-fit", "", "write this file when the build is fit to become the baseline the next build compares against")
	flag.Parse()

	if *catalogPath == "" {
		log.Fatalln("-tcg-catalog is required: the dump carries the printings and the ids")
	}
	// The stamped promos are the only rows of theirs anyone publishes, and
	// losing them is not a failure this build would otherwise notice: 83
	// cards out of 44,190 is a fifth of a percent, well inside the 1% the
	// -against guard allows, so a run without the catalog would publish a
	// datastore quietly missing them and unprice 92 products of a vendor
	// that sells them. It refuses instead.
	if *cardmarketCatalogPath == "" {
		log.Fatalln("-cardmarket-catalog is required: nothing else lists the stamped promos, and their loss is too small for the baseline guard to catch")
	}
	catalogData, err := os.ReadFile(*catalogPath)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	var catalog tcgplayer.CatalogDump
	err = json.Unmarshal(catalogData, &catalog)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	if catalog.Category.CategoryID != pokemonCategory {
		log.Fatalf("tcg catalog: category %d, want %d (wrong game's dump)",
			catalog.Category.CategoryID, pokemonCategory)
	}

	setsData, err := loadTcgdexCached(*tcgdexSets, *upstreamCache, "tcgdex-sets.json", tcgdexSetsQuery)
	if err != nil {
		log.Fatalln("tcgdex sets:", err)
	}
	var setsResponse struct {
		Sets []tcgdexSet `json:"sets"`
	}
	err = decodeEnvelope(setsData, &setsResponse)
	if err != nil {
		log.Fatalln("tcgdex sets:", err)
	}
	// A response older than the query answers every field the query did not
	// used to ask for with nothing, and says so nowhere: -tcgdex-sets reads
	// a file whatever the query has become, and the cache is read back
	// verbatim when the API is unreachable. A file written before the
	// symbol was asked for yields a datastore whose sets all draw their own
	// badge, which looks like a datastore and is one field short of it.
	if len(setsResponse.Sets) > 0 {
		var withSymbol int
		for _, set := range setsResponse.Sets {
			if set.Symbol != "" {
				withSymbol++
			}
		}
		if withSymbol == 0 {
			log.Printf("tcgdex sets: not one of the %d sets carries a symbol, so this response predates the query asking for one; the sets will carry none",
				len(setsResponse.Sets))
		}
	}
	cardsData, err := loadTcgdexCached(*tcgdexCards, *upstreamCache, "tcgdex-cards.json", tcgdexCardsQuery)
	if err != nil {
		log.Fatalln("tcgdex cards:", err)
	}
	var cardsResponse struct {
		Cards []tcgdexCard `json:"cards"`
	}
	err = decodeEnvelope(cardsData, &cardsResponse)
	if err != nil {
		log.Fatalln("tcgdex cards:", err)
	}

	var dexSets []tcgdexSet
	// dexSetKnown is the sets a tcgdex card may be counted under: the
	// digital Pocket ones are dropped here, and a card of theirs is not a
	// printing this datastore is missing.
	dexSetKnown := map[string]bool{}
	for _, set := range setsResponse.Sets {
		if set.Serie.ID == tcgpSerie {
			continue
		}
		dexSets = append(dexSets, set)
		dexSetKnown[set.ID] = true
	}
	log.Printf("catalog: %d groups, %d products; tcgdex: %d sets (%d after dropping %s), %d cards",
		len(catalog.Groups), len(catalog.Products), len(setsResponse.Sets), len(dexSets),
		tcgpSerie, len(cardsResponse.Cards))

	// Assign every group its set code, in group-id order so the group that
	// claimed an abbreviation keeps it and only the later arrival is
	// marked. Counting the abbreviations first and suffixing every sharer
	// would rewrite an existing set's code — and every id filed under it —
	// the day TCGplayer adds a group carrying the same abbreviation. A
	// blank abbreviation gets a code minted from the group id.
	groups := append([]tcgplayer.Group(nil), catalog.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GroupID < groups[j].GroupID
	})
	setCodes := map[int]string{}
	usedCodes := map[string]bool{}
	var minted, suffixed int
	for _, group := range groups {
		code := strings.ToUpper(setCodeOf(group.Abbreviation))
		if code == "" {
			code = fmt.Sprintf("G%d", group.GroupID)
			minted++
			log.Printf("%s: no abbreviation, set code %s minted", group.Name, code)
		}
		if usedCodes[code] {
			code = fmt.Sprintf("%s-%d", code, group.GroupID)
			suffixed++
			log.Printf("%s: abbreviation %s already taken, set code %s minted",
				group.Name, group.Abbreviation, code)
		}
		if usedCodes[code] {
			log.Fatalf("set code %s not unique; refusing to guess further", code)
		}
		usedCodes[code] = true
		setCodes[group.GroupID] = code
	}
	log.Printf("set codes: %d minted for blank abbreviations, %d deduplicated", minted, suffixed)

	// A championship is a set. The group code is the stem and the year the
	// suffix, so "WCD" becomes WCD2004 through WCD2025, and every product
	// of the shelf - cards, decks and code cards alike - is filed under the
	// year it names. productSetCode holds the products whose set is not
	// their group's, which is only ever this.
	worldsGroup := map[int]bool{}
	for _, group := range groups {
		if worldsGroupRe.MatchString(group.Name) {
			worldsGroup[group.GroupID] = true
		}
	}
	productSetCode := map[int]string{}
	worldsYears := map[int]string{}
	worldsSets := map[string]string{}
	var worldsUndated int
	for _, product := range catalog.Products {
		if !worldsGroup[product.GroupID] {
			continue
		}
		year := worldsYear(product.Name)
		if year == "" {
			worldsUndated++
			log.Printf("world championships: %q (%d) names no year and stays on the shelf",
				product.Name, product.ProductID)
			continue
		}
		code := setCodes[product.GroupID] + year
		if !usedCodes[code] {
			usedCodes[code] = true
		}
		productSetCode[product.ProductID] = code
		worldsYears[product.ProductID] = year
		worldsSets[year] = code
	}
	if len(worldsSets) > 0 {
		log.Printf("world championships: %d products over %d years, %d naming none",
			len(productSetCode), len(worldsSets), worldsUndated)
	}
	setCodeFor := func(p tcgplayer.Product) string {
		if code, split := productSetCode[p.ProductID]; split {
			return code
		}
		return setCodes[p.GroupID]
	}

	// Join each group to its tcgdex set by normalized name, retrying with
	// the short-code prefix stripped. Ambiguous or missing joins nothing:
	// a wrong annotation is worse than a missing one.
	dexByName := map[string][]*tcgdexSet{}
	for i := range dexSets {
		key := normalizeName(dexSets[i].Name)
		dexByName[key] = append(dexByName[key], &dexSets[i])
	}
	joinedSets := map[int]*tcgdexSet{}
	for _, group := range groups {
		name := group.Name
		alias, aliased := setAliases[name]
		if aliased {
			name = alias
		}
		candidates := dexByName[normalizeName(name)]
		if len(candidates) == 0 {
			candidates = dexByName[normalizeName(stripSetPrefix(name))]
		}
		// The EX era wears a bare "EX " word tcgdex does not carry.
		if len(candidates) == 0 && strings.HasPrefix(name, "EX ") {
			candidates = dexByName[normalizeName(strings.TrimPrefix(name, "EX "))]
		}
		switch len(candidates) {
		case 0:
			log.Printf("tcgdex: %q (%s) has no set match, unjoined", group.Name, setCodes[group.GroupID])
		case 1:
			joinedSets[group.GroupID] = candidates[0]
		default:
			log.Printf("tcgdex: %q (%s) matches %d sets, unjoined", group.Name, setCodes[group.GroupID], len(candidates))
		}
	}
	log.Printf("tcgdex set join: %d of %d groups", len(joinedSets), len(groups))

	// Resolve every group's release date: a real (midnight) publishedOn is
	// authoritative, the joined tcgdex set fills the placeholders, and what
	// neither source can date stays empty rather than guessed.
	// A group with no product is a legacy husk TCGplayer keeps around; it
	// emits no set below, so it is not worth dating, joining, or being
	// reported as undated.
	productsIn := map[int]int{}
	for _, product := range catalog.Products {
		productsIn[product.GroupID]++
	}

	releaseDates := map[int]string{}
	// The groups a source dates on its own, which is what makes a hand row
	// for one of them dead rather than merely unused this run.
	datedBySource := map[int]bool{}
	var placeholders, filled int
	for _, group := range groups {
		if productsIn[group.GroupID] == 0 {
			continue
		}
		if hasDate(group) {
			releaseDates[group.GroupID] = group.ReleaseDate()
			datedBySource[group.GroupID] = true
			continue
		}
		placeholders++
		dex := joinedSets[group.GroupID]
		if dex != nil && dex.ReleaseDate != "" {
			releaseDates[group.GroupID] = dex.ReleaseDate
			datedBySource[group.GroupID] = true
			filled++
			log.Printf("%s (%s): release date %s filled from tcgdex",
				group.Name, setCodes[group.GroupID], dex.ReleaseDate)
			continue
		}
		if date, found := handDates[group.GroupID]; found {
			releaseDates[group.GroupID] = date
			filled++
			log.Printf("%s (%s): release date %s filled by hand",
				group.Name, setCodes[group.GroupID], date)
			continue
		}
		log.Printf("%s (%s): no release date anywhere, left undated", group.Name, setCodes[group.GroupID])
	}
	reportStaleHandTables(catalog, datedBySource)
	log.Printf("release dates: %d placeholders, %d filled, %d left empty",
		placeholders, filled, placeholders-filled)

	checkPinnedPrintings(&catalog)
	printings := printingNames(&catalog)

	// Split the products: every single becomes card entries per sku
	// printing, Sealed Products become sealed. "N/A" is a spelling of no
	// number; the unnumbered are real singles and stay.
	var singles []single
	var numberLedQuals int
	var sealedProducts []tcgplayer.Product
	var codeCards, unnumbered int
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			// Every card product the catalog has ever carried prices at
			// least one English sku, and a product with none has no
			// printing to file an entry under: stop rather than drop it.
			log.Fatalf("no English sku printing: %q (%d) has no entry to carry it",
				product.Name, product.ProductID)
		}
		if product.Extended("Rarity") == codeCardRarity {
			codeCards++
		}
		num := product.Extended("Number")
		if strings.EqualFold(num, "N/A") {
			num = ""
		}
		if num == "" {
			unnumbered++
		}
		one, numberLed := decompose(product, num, worldsYears[product.ProductID])
		numberLedQuals += numberLed
		singles = append(singles, one)
	}
	log.Printf("singles: %d kept (%d unnumbered, %d code cards), %d sealed",
		len(singles), unnumbered, codeCards, len(sealedProducts))
	var unrated int
	for _, s := range singles {
		if s.product.Extended("Rarity") == "" {
			unrated++
			log.Printf("no rarity: %q (%d) carried without one", s.product.Name, s.product.ProductID)
		}
	}
	if unrated > 0 {
		log.Printf("no rarity: %d products, carried and logged rather than refused", unrated)
	}
	if len(singles) == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}

	// Buckets by collector number within the group, which the collision
	// guard below reads. The name-versus-variant call is not made here any
	// more: it is made per card against tcgdex, which knows what a card is
	// named without having to infer it from what TCGplayer happens to sell.
	byNumber := map[string][]*single{}
	for i := range singles {
		if singles[i].number == "" {
			continue
		}
		byNumber[electionKey(&singles[i])] = append(byNumber[electionKey(&singles[i])], &singles[i])
	}

	// The name is the bare name. Every qualifier the catalog hangs off it
	// is a property of the printing and leaves as a variant label, which is
	// what the entry publishes as its promo types - the one exception being
	// a qualifier tcgdex prints as part of the name itself.
	pokemon := pokemonNames(cardsResponse.Cards)
	nameQuals := dexNameQuals(cardsResponse.Cards)
	var keptQuals, droppedLabels int
	for i := range singles {
		s := &singles[i]
		name := []string{s.baseName}
		var variant []qual
		for _, q := range s.quals {
			if written, upstream := nameQuals[qualNameKey(s.baseName, q.text)]; upstream {
				name = append(name, written)
				keptQuals++
				continue
			}
			// "Zacian V-UNION [Set of 4]" is four cards TCGplayer types as
			// one card product. How many cards are in a product is not a
			// property of a printing, so it stays in the name it was
			// written in rather than becoming a promo type on a single.
			if setOfRe.MatchString(q.text) {
				name = append(name, q.String())
				keptQuals++
				continue
			}
			variant = append(variant, q)
		}
		s.baseName = strings.Join(name, " ")
		s.quals = nil
		for _, q := range variant {
			if droppedQuals[strings.ToLower(q.text)] || isBareLetter(q.text) {
				droppedLabels++
				continue
			}
			s.quals = append(s.quals, q)
		}
	}
	if droppedLabels > 0 {
		log.Printf("promo types: %d labels dropped for saying nothing a reader needs", droppedLabels)
	}
	if numberLedQuals > 0 {
		log.Printf("name qualifiers: %d opened with the card's own number, which is dropped", numberLedQuals)
	}
	log.Printf("name qualifiers: %d kept because tcgdex names the card that way", keptQuals)

	// One label, one spelling. The catalog writes the same qualifier several
	// ways - "regional championships" beside "regional championship",
	// "eb games exclusive" beside "ebgames exclusive", "#30 holo" beside
	// "#30 - holo" - and a query naming one of them misses every printing
	// filed under the others, which is the same fault oneCosmos was written
	// for and the same fix, only counted rather than listed.
	//
	// Spellings are folded to the one the catalog uses most, so the winner
	// is the wording a listing is likeliest to arrive in, and ties go to
	// whichever sorts first so a rebuild folds the same way twice.
	// "Stamp" against "Stamped", where the catalog writes both of the same
	// marking: prismatic evolutions, stellar crown, twilight masquerade and
	// pokemon horizons each carry the pair. Only those are touched. Seven
	// more end in "Stamp" with no twin anywhere - "Left Stamp", "SDCC
	// Stamp", "What's Your Favorite Stamp" - and a blanket suffix rule
	// would rewrite all of them to say something no card says.
	written := map[string]bool{}
	for i := range singles {
		for _, q := range singles[i].quals {
			written[q.text] = true
		}
	}
	// A dash inside a parenthesis joins two labels where both halves are
	// labels other cards already carry: "Deoxys (Delta Species - Attack
	// Forme)" is a Delta Species card in its Attack Forme, and "Espeon -
	// 2/90 (HGSS Undaunted - Cracked Ice Holo)" a set and a surface. The
	// peeler reads the outer delimiter and stops, so both arrived whole.
	//
	// Both halves have to be known before either is taken, which is what
	// keeps this off a dash that is part of a label rather than between
	// two: "Jose Cruz Galindo-Resendiz" has no space around its dash and
	// "2006-2007" none either, and neither half of either is a label.
	var dashSplit int
	for i := range singles {
		var out []qual
		for _, q := range singles[i].quals {
			head, tail, found := strings.Cut(q.text, " - ")
			if !found || !written[head] || !written[tail] {
				out = append(out, q)
				continue
			}
			out = append(out,
				qual{text: head, bracket: q.bracket},
				qual{text: tail, bracket: q.bracket})
			dashSplit++
		}
		singles[i].quals = out
	}
	if dashSplit > 0 {
		log.Printf("promo types: %d labels split on a dash joining two the catalog writes apart", dashSplit)
	}

	// A signature is a treatment and the name in front of it is the player,
	// who is a label 26 other cards of that championship already carry:
	// "Regidrago VSTAR - 2024 (Evan Pavelski Gold Signature)".
	var goldSplit int
	for i := range singles {
		var out []qual
		for _, q := range singles[i].quals {
			head, ok := strings.CutSuffix(q.text, " Gold Signature")
			if !ok || !written[head] {
				out = append(out, q)
				continue
			}
			out = append(out,
				qual{text: head, bracket: q.bracket},
				qual{text: "Gold Signature", bracket: q.bracket})
			goldSplit++
		}
		singles[i].quals = out
	}
	if goldSplit > 0 {
		log.Printf("promo types: %d signatures split from the player who signed", goldSplit)
	}

	var stamped int
	for i := range singles {
		for j, q := range singles[i].quals {
			if !strings.HasSuffix(q.text, " Stamp") || !written[q.text+"ed"] {
				continue
			}
			singles[i].quals[j].text = q.text + "ed"
			stamped++
		}
	}
	if stamped > 0 {
		log.Printf("promo types: %d labels say Stamped, beside the twin that already did", stamped)
	}

	spellings := map[string]map[string]int{}
	for i := range singles {
		for _, q := range singles[i].quals {
			key := foldQualKey(q.text)
			if spellings[key] == nil {
				spellings[key] = map[string]int{}
			}
			spellings[key][q.text]++
		}
	}
	folded := map[string]string{}
	for key, seen := range spellings {
		if len(seen) < 2 {
			continue
		}
		winner, tied := "", false
		for text := range seen {
			switch {
			case winner == "" || seen[text] > seen[winner]:
				winner, tied = text, false
			case seen[text] == seen[winner]:
				tied = true
				if text < winner {
					winner = text
				}
			}
		}
		if tied {
			// Nothing to learn from: the catalog has shown no preference,
			// so folding would be picking one spelling over another on
			// nothing but sort order.
			var texts []string
			for text := range seen {
				texts = append(texts, text)
			}
			sort.Strings(texts)
			log.Printf("promo types: %q are used alike and are left apart", texts)
			continue
		}
		for text := range seen {
			if text != winner {
				folded[text] = winner
				log.Printf("promo types: %q folds into %q (%d against %d)",
					text, winner, seen[text], seen[winner])
			}
		}
		_ = key
	}
	if len(folded) > 0 {
		var refolded int
		for i := range singles {
			for j, q := range singles[i].quals {
				if winner, fold := folded[q.text]; fold {
					singles[i].quals[j].text = winner
					refolded++
				}
			}
		}
		log.Printf("promo types: %d spellings folded away over %d labels", len(folded), refolded)
	}

	// Two labels naming one thing, which the election cannot reach: it
	// compares spellings that fold to one key, and these fold to two - a
	// word apart, or one use each with nothing to learn from. It reads last
	// so it never has to name a spelling the election was going to fix
	// anyway: "Toys R' Us Promo" folds into "Toys R Us Promo" first, and
	// only then does this take the word "promo" off it.
	var handFixed int
	for i := range singles {
		for j, q := range singles[i].quals {
			if fixed, hand := qualSpellings[strings.ToLower(q.text)]; hand {
				singles[i].quals[j].text = fixed
				handFixed++
			}
		}
	}
	if handFixed > 0 {
		log.Printf("promo types: %d labels named by hand, where frequency had nothing to say", handFixed)
	}

	// A year in front of a label the vocabulary already carries is two
	// labels: "2011 Pokemon League" is the season and the tier, and 122
	// other cards say the tier without a season. Splitting keeps the year,
	// which nothing else on these cards records - they are energies with no
	// number - and stops the tier reading as a thing of its own.
	yearLead := regexp.MustCompile(`^((?:19|20)\d{2}(?:-\d{2,4})?)\s+(\S.*)$`)
	var yearSplit int
	for i := range singles {
		var out []qual
		for _, q := range singles[i].quals {
			m := yearLead.FindStringSubmatch(q.text)
			// A year variantOnlyQuals drops would leave the promo types
			// with no date at all, which is the opposite of the point:
			// "2014 Movie Promo" is one Pikachu nothing else dates, where
			// the three Champions Festivals are dated by their numbers.
			if m != nil && variantOnlyQuals[strings.ToLower(m[1])] {
				m = nil
			}
			if m == nil || !written[m[2]] {
				out = append(out, q)
				continue
			}
			out = append(out,
				qual{text: m[1], bracket: q.bracket},
				qual{text: m[2], bracket: q.bracket})
			yearSplit++
		}
		singles[i].quals = out
	}
	if yearSplit > 0 {
		log.Printf("promo types: %d labels split into the year and what it dates", yearSplit)
	}

	// The collision guard: a pre-election drop must not leave two products
	// of a bucket with the same (name, variant, rarity), so a colliding
	// product takes its dropped qualifiers back as variant. The keys are
	// re-derived until no restore fires, because a restore can restore the
	// very text its sibling already carries, or newly collide with a third
	// product; what still collides with nothing left to restore is named
	// here and refused by validate.
	variantOf := func(s *single) string {
		var texts []string
		for _, q := range s.quals {
			texts = append(texts, q.text)
		}
		return strings.Join(texts, " ")
	}
	keyOf := func(s *single) string {
		return s.baseName + "|" + variantOf(s) + "|" + s.product.Extended("Rarity")
	}
	var restoredDrops, identicalPairs int
	for _, bucket := range byNumber {
		if len(bucket) < 2 {
			continue
		}
		for {
			seen := map[string][]*single{}
			for _, s := range bucket {
				seen[keyOf(s)] = append(seen[keyOf(s)], s)
			}
			restored := false
			for _, s := range bucket {
				if len(seen[keyOf(s)]) < 2 || len(s.dropped) == 0 {
					continue
				}
				s.quals = append(s.quals, s.dropped...)
				s.dropped = nil
				restored = true
				restoredDrops++
				log.Printf("collision guard: %q (%d) keeps its dropped qualifiers as variant",
					s.product.Name, s.product.ProductID)
			}
			if restored {
				continue
			}
			for _, s := range bucket {
				colliding := seen[keyOf(s)]
				if len(colliding) < 2 || colliding[0] != s {
					continue
				}
				identicalPairs++
				log.Printf("collision guard: %d products of %s|%s wear one identity: %q",
					len(colliding), setCodeFor(s.product), s.number, s.product.Name)
			}
			break
		}
	}
	if restoredDrops > 0 || identicalPairs > 0 {
		log.Printf("collision guard: %d drops restored, %d identical groups left", restoredDrops, identicalPairs)
	}

	// Join each single to its tcgdex card by localId against the number's
	// numerator within the joined set, and cross-check the finish story the
	// two sources tell. Annotation only: identity never depends on it.
	dexByLocal := map[string][]*tcgdexCard{}
	for i := range cardsResponse.Cards {
		card := &cardsResponse.Cards[i]
		key := card.Set.ID + "|" + numeratorKey(card.LocalID)
		dexByLocal[key] = append(dexByLocal[key], card)
	}
	// An axis tcgdex flags on none of a set's cards is one it does not
	// track there — Lost Thunder has no reverse flags at all — so a sku-only
	// reading against such a set is silence, not disagreement.
	type axes struct {
		normal, reverse, holo, firstEdition bool
	}
	setAxes := map[string]axes{}
	for _, card := range cardsResponse.Cards {
		a := setAxes[card.Set.ID]
		a.normal = a.normal || card.Variants.Normal
		a.reverse = a.reverse || card.Variants.Reverse
		a.holo = a.holo || card.Variants.Holo
		a.firstEdition = a.firstEdition || card.Variants.FirstEdition
		setAxes[card.Set.ID] = a
	}
	sort.Slice(singles, func(i, j int) bool {
		return singles[i].product.ProductID < singles[j].product.ProductID
	})
	dexCards := map[int]*tcgdexCard{}
	var ambiguousLocal int
	crossCheck := map[string]int{}
	for i := range singles {
		s := &singles[i]
		dex := joinedSets[s.product.GroupID]
		if dex == nil || s.number == "" {
			continue
		}
		numerator := strings.SplitN(s.number, "/", 2)[0]
		candidates := dexByLocal[dex.ID+"|"+numeratorKey(numerator)]
		if len(candidates) > 1 {
			ambiguousLocal++
			continue
		}
		if len(candidates) == 0 {
			continue
		}
		card := candidates[0]
		dexCards[s.product.ProductID] = card
		names := printings[s.product.ProductID]
		// The WotC-era printing names spell the axes differently: 1st
		// Edition and Unlimited are that era's normals, their Holofoil
		// forms its holos.
		hasNormal := sliceContains(names, "Normal") ||
			sliceContains(names, "1st Edition") || sliceContains(names, "Unlimited")
		hasHolo := sliceContains(names, "Holofoil") ||
			sliceContains(names, "1st Edition Holofoil") || sliceContains(names, "Unlimited Holofoil")
		tracked := setAxes[card.Set.ID]
		checks := []struct {
			axis    string
			tcgdex  bool
			tracked bool
			printed bool
		}{
			{"normal", card.Variants.Normal, tracked.normal, hasNormal},
			{"reverse", card.Variants.Reverse, tracked.reverse, sliceContains(names, "Reverse Holofoil")},
			{"holo", card.Variants.Holo, tracked.holo, hasHolo},
			{"firstEdition", card.Variants.FirstEdition, tracked.firstEdition,
				sliceContains(names, "1st Edition") || sliceContains(names, "1st Edition Holofoil")},
		}
		for _, c := range checks {
			if c.tcgdex && !c.printed {
				crossCheck[c.axis+" tcgdex-only"]++
			}
			if c.printed && !c.tcgdex && c.tracked {
				crossCheck[c.axis+" sku-only"]++
			}
		}
	}
	log.Printf("tcgdex card join: %d of %d singles (%.1f%%), %d ambiguous localIds skipped",
		len(dexCards), len(singles), 100*float64(len(dexCards))/float64(len(singles)), ambiguousLocal)

	// The other direction, which nothing counted before: a tcgdex card no
	// product joined is a card the catalog does not sell, and a tcgdex set
	// no group joined is a whole set of them. The join rate above measures
	// only how much of the catalog tcgdex could annotate, so a set upstream
	// carries and TCGplayer does not was invisible - it neither annotated
	// anything nor showed up as a gap. These are what the minting below
	// adds, so the datastore holds both sources rather than the catalog
	// alone.
	joinedGroups := map[string]bool{}
	for _, dex := range joinedSets {
		joinedGroups[dex.ID] = true
	}
	carried := map[string]bool{}
	for _, card := range dexCards {
		carried[card.ID] = true
	}
	uncarriedBySet := map[string]int{}
	var uncarried, inUnjoinedSets int
	var mintable []*tcgdexCard
	for i := range cardsResponse.Cards {
		card := &cardsResponse.Cards[i]
		if !dexSetKnown[card.Set.ID] || carried[card.ID] {
			continue
		}
		uncarried++
		uncarriedBySet[card.Set.ID]++
		if !joinedGroups[card.Set.ID] {
			inUnjoinedSets++
		}
		mintable = append(mintable, card)
	}
	// Stable order, so unchanged data keeps producing byte-identical output.
	sort.Slice(mintable, func(i, j int) bool {
		return mintable[i].ID < mintable[j].ID
	})
	log.Printf("tcgdex cards the catalog has no product for: %d of %d over %d sets (%d of them in the %d sets no group joined)",
		uncarried, len(cardsResponse.Cards), len(uncarriedBySet), inUnjoinedSets,
		len(dexSets)-len(joinedGroups))
	var worst []string
	for id := range uncarriedBySet {
		worst = append(worst, id)
	}
	sort.Slice(worst, func(i, j int) bool {
		if uncarriedBySet[worst[i]] != uncarriedBySet[worst[j]] {
			return uncarriedBySet[worst[i]] > uncarriedBySet[worst[j]]
		}
		return worst[i] < worst[j]
	})
	for i, id := range worst {
		if i >= 10 {
			log.Printf("tcgdex uncarried: %d more sets", len(worst)-i)
			break
		}
		log.Printf("tcgdex uncarried: %s holds %d", id, uncarriedBySet[id])
	}
	var checkKeys []string
	for k := range crossCheck {
		checkKeys = append(checkKeys, k)
	}
	sort.Strings(checkKeys)
	for _, k := range checkKeys {
		log.Printf("tcgdex variant cross-check: %s on %d products", k, crossCheck[k])
	}

	// Emit. Sets are the catalog groups; card ids embed the product id so
	// they survive any upstream renumbering, and the finish suffix so each
	// price point is its own entry.
	// An empty group is skipped, its code left claimed so nothing renames
	// while it is empty and the set appears already-coded the day
	// TCGplayer files a product there.
	sets := map[string]any{}
	promoted := promoGroups(catalog)
	var promoSets, skippedEmpty, symboled int
	for _, group := range groups {
		if productsIn[group.GroupID] == 0 {
			skippedEmpty++
			continue
		}
		if worldsGroup[group.GroupID] && worldsUndated == 0 {
			// One set per championship is emitted below. The shelf keeps a
			// set of its own only while a product on it names no year, so
			// nothing is ever filed under a code no set carries.
			continue
		}
		set := map[string]any{
			"name":        group.Name,
			"releaseDate": releaseDates[group.GroupID],
		}
		if group.Abbreviation != "" {
			set["abbreviation"] = group.Abbreviation
		}
		// The mark the set's cards print, from the tcgdex set the group
		// joined. tcgdex hands back an extension-less asset URL and serves
		// several encodings off it; webp is the smallest, and the one the
		// card images already ask for. A group that joined no set, or joined
		// one tcgdex holds no symbol for, carries none, and whatever renders
		// this falls back to drawing the set code.
		if dex := joinedSets[group.GroupID]; dex != nil && dex.Symbol != "" {
			set["symbol"] = dex.Symbol + ".webp"
			symboled++
		}
		// The type is what tells the matcher a printing is promotional, so
		// only the wholly promotional groups carry it.
		if promoted[group.GroupID] {
			set["type"] = "promo"
			promoSets++
		}
		sets[setCodes[group.GroupID]] = set
	}
	for year, code := range worldsSets {
		sets[code] = map[string]any{
			"name":        year + " World Championship Decks",
			"releaseDate": worldsReleaseDate(year),
		}
	}
	if len(worldsSets) > 0 {
		log.Printf("world championships: %d sets, %s to %s",
			len(worldsSets), worldsReleaseDate(slices.Min(slices.Collect(maps.Keys(worldsSets)))),
			worldsReleaseDate(slices.Max(slices.Collect(maps.Keys(worldsSets)))))
	}
	if skippedEmpty > 0 {
		log.Printf("sets: %d empty groups hold no product and are skipped", skippedEmpty)
	}
	log.Printf("promotional sets: %d of %d", promoSets, len(groups))

	// The sets a minted card is filed under. A tcgdex set a group joined is
	// that group's set, under the code the group already claimed, so a
	// minted card lands beside the printings TCGplayer does sell. A tcgdex
	// set no group joined is a set TCGplayer files nothing for, and is
	// minted from tcgdex's own id, name and release date, deduplicated
	// against the codes the groups already hold so nothing folds onto them.
	dexSetByID := map[string]*tcgdexSet{}
	for i := range dexSets {
		dexSetByID[dexSets[i].ID] = &dexSets[i]
	}
	groupCodeByDexSet := map[string]string{}
	for groupID, dex := range joinedSets {
		if _, taken := groupCodeByDexSet[dex.ID]; !taken {
			groupCodeByDexSet[dex.ID] = setCodes[groupID]
		}
	}
	mintedSetCode := map[string]string{}
	var mintedSets int
	for _, card := range mintable {
		if _, decided := mintedSetCode[card.Set.ID]; decided {
			continue
		}
		if code, joined := groupCodeByDexSet[card.Set.ID]; joined {
			mintedSetCode[card.Set.ID] = code
			continue
		}
		dex := dexSetByID[card.Set.ID]
		code := strings.ToUpper(setCodeOf(card.Set.ID))
		if code == "" {
			log.Fatalf("tcgdex set %q reduces to no set code", card.Set.ID)
		}
		if usedCodes[code] {
			code = code + "-DEX"
			log.Printf("tcgdex set %s: code already taken, minted set code %s", card.Set.ID, code)
		}
		if usedCodes[code] {
			log.Fatalf("minted set code %s still not unique; refusing to guess further", code)
		}
		usedCodes[code] = true
		mintedSetCode[card.Set.ID] = code
		set := map[string]any{"name": card.Set.ID, "releaseDate": ""}
		if dex != nil {
			set["name"] = dex.Name
			set["releaseDate"] = dex.ReleaseDate
			if dex.Symbol != "" {
				set["symbol"] = dex.Symbol + ".webp"
				symboled++
			}
		}
		sets[code] = set
		mintedSets++
	}
	if mintedSets > 0 {
		log.Printf("sets minted for tcgdex sets no group joined: %d", mintedSets)
	}
	// The symbols tcgdex has none of, from pokemontcg.io, which carries one
	// for every set it lists. Only the sets still without one are asked
	// about, so tcgdex stays the first word wherever it has one.
	//
	// Two joins, and the second is why the first is not enough on its own:
	// an exact name reaches the McDonald's collections and the reprint sets
	// tcgdex leaves blank, and a ptcgo code reaches the sets whose names
	// the two sources write differently. The code alone would be reckless -
	// "BST" is this catalog's EX Battle Stadium of 2004 and pokemontcg.io's
	// Battle Styles of 2021, seventeen years apart and three letters alike -
	// so the release day has to agree with it.
	//
	// The URL is taken as given rather than built on. pokemontcg.io serves
	// PNG where tcgdex serves webp, four of its symbols sit on another host
	// entirely, and asking its path for a webp answers 404 with a 186KB
	// body typed image/png.
	if ptcg, err := loadPokemontcgSets(*pokemontcgSets, *upstreamCache); err != nil {
		log.Printf("pokemontcg.io: %v; the sets tcgdex has no symbol for keep none", err)
	} else {
		byName := map[string]*pokemontcgSet{}
		byCodeDate := map[string]*pokemontcgSet{}
		for i := range ptcg {
			set := &ptcg[i]
			if set.Images.Symbol == "" {
				continue
			}
			byName[mtgmatcherNormalize(set.Name)] = set
			if set.PtcgoCode != "" {
				byCodeDate[strings.ToUpper(set.PtcgoCode)+"|"+pokemontcgDate(set.ReleaseDate)] = set
			}
		}
		var byNameHits, byCodeHits int
		for code, entry := range sets {
			set, isMap := entry.(map[string]any)
			if !isMap || set["symbol"] != nil {
				continue
			}
			name, _ := set["name"].(string)
			date, _ := set["releaseDate"].(string)
			if found, ok := byName[mtgmatcherNormalize(name)]; ok {
				set["symbol"] = found.Images.Symbol
				byNameHits++
				symboled++
				continue
			}
			if found, ok := byCodeDate[strings.ToUpper(code)+"|"+date]; ok {
				set["symbol"] = found.Images.Symbol
				byCodeHits++
				symboled++
			}
		}
		if byNameHits+byCodeHits > 0 {
			log.Printf("set symbols: %d filled from pokemontcg.io (%d by name, %d by ptcgo code and release day)",
				byNameHits+byCodeHits, byNameHits, byCodeHits)
		}
	}
	log.Printf("set symbols: %d of %d sets carry one", symboled, len(sets))

	// The coverage contract: every product the catalog types as a card,
	// with the sku printings it is sold in. validate reads it back off the
	// encoded output, so a product no rule here carried fails the build
	// instead of quietly leaving the datastore.
	catalogFinishes := map[int][]string{}
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			continue
		}
		catalogFinishes[product.ProductID] = printings[product.ProductID]
	}

	// The sets this datastore carries, by name, and the ones that are a
	// shelf rather than a set. "Deck Exclusives" and "Blister Exclusives"
	// hold cards sold only inside a deck or a blister, and the label on
	// each says which set it was pulled from: "Metagross - 11/101 (EX
	// Hidden Legends)" is numbered out of 101 and EX Hidden Legends holds
	// 101, which is true of 111 of the 116 that name a set we carry.
	//
	// That is provenance, not promotion, so it leaves the promo types and
	// stays the variant it already was. Scoped to those shelves because
	// the same test unscoped reads "Gym Challenge" as the 2000 set rather
	// than the tournament tier, and "Base Set" and "Jungle" off cards that
	// only picture them.
	setNames := map[string]bool{}
	exclusiveShelf := map[string]bool{}
	for code, entry := range sets {
		set, isMap := entry.(map[string]any)
		if !isMap {
			continue
		}
		name, _ := set["name"].(string)
		if name == "" {
			continue
		}
		setNames[mtgmatcherNormalize(name)] = true
		// And without its era, because a label writes the set either way:
		// "Ruby & Sapphire" for our "EX Ruby and Sapphire". The same head
		// test namesASet uses to read past one.
		if head, rest, found := strings.Cut(name, " "); found && len(head) <= 4 && strings.Contains(rest, " ") {
			setNames[mtgmatcherNormalize(rest)] = true
		}
		if strings.HasSuffix(name, "Exclusives") {
			exclusiveShelf[code] = true
		}
	}

	var cards []any
	for i := range singles {
		s := &singles[i]
		productID := s.product.ProductID

		image := imageURL(s.product.ImageURL)
		dex := dexCards[productID]
		if dex != nil && dex.Image != "" {
			image = dex.Image + "/high.webp"
		}
		for _, finish := range printings[productID] {
			suffix := finishSuffixFor(finish)
			entry := map[string]any{
				"id":      idBase(s.number, productID) + suffix,
				"name":    handName(s.product.ProductID, s.baseName),
				"setCode": setCodeFor(s.product),
				"rarity":  s.product.Extended("Rarity"),
				"finish":  finish,
				"image":   image,
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			// The catalog's own wording, kept where it differs from the
			// name published above. A storefront copies TCGplayer's
			// product name verbatim - number, qualifiers and all - so
			// something has to hold the spelling a listing will arrive
			// in once the name here stops carrying it.
			if s.product.Name != entry["name"] {
				entry["originalName"] = s.product.Name
			}
			if s.number != "" {
				emitNumber(entry, numberOf(s.number))
			}
			cardType := s.product.Extended("Card Type")
			if cardType != "" {
				entry["type"] = cardType
			}
			variant := variantOf(s)
			if variant != "" {
				entry["variant"] = variant
				// The same labels as a list: joined, "Full Art Staff"
				// cannot be read back into the two tags it holds, and the
				// matcher needs them whole to declare and to match on.
				// A printing whose only label was a Pokemon has a variant
				// and no promo types, so the key stays off rather than
				// carrying an empty list.
				if tags := promoTypesOf(s, pokemon, setNames, exclusiveShelf[setCodeFor(s.product)]); len(tags) > 0 {
					entry["promoTypes"] = tags
				}
			}
			if dex != nil {
				entry["tcgdexId"] = dex.ID
			}
			cards = append(cards, entry)
		}
	}

	// Mint the cards tcgdex publishes that the catalog has no product for -
	// the trainer kits, the McDonald's promos, whole sets TCGplayer files
	// no group for. A datastore leaving them out leaves every listing of
	// one unresolvable, so it carries the sum of both sources rather than
	// the catalog alone. A minted entry names no product because there is
	// none: nothing prices it, and the loader groups an entry without a
	// product id by its own id with the finish suffix stripped, which is
	// exactly how these are built. Each variant tcgdex flags becomes its
	// own entry, as a product's sku printings do, and a card tcgdex flags
	// nothing on still gets its plain one.
	// A minted entry numbers itself from tcgdex's localId, which carries no
	// denominator, so it emits no total at all - and the printed total is
	// what tells "8" in Base Set apart from "8" in any other set. Where
	// every catalog entry in the set agrees on one total, that total is the
	// set's, and a card printed alongside them carries the same one. Sets
	// whose entries disagree are left alone rather than guessed at: the
	// promo shelves gather cards printed for several sets, and no single
	// total is right for all of them.
	//
	// A number opening with a letter code is its own numbering series with
	// its own denominator - Legendary Treasures' RC1 is printed "RC1/RC25"
	// beside a main set of 113, and the H subsets of Aquapolis and Skyridge
	// carry an H32 the catalog spells out. Those take the set's total only
	// by coincidence, so only a number opening on a digit is filled. A
	// trailing letter is a different thing: 75a is the set's own card 75 in
	// alternate art and shares its denominator.
	totalBySet := totalsBySet(cards)

	var mintedCards, mintedWithoutArt int
	var mintedTotals int
	// The sets tcgdex carries that the catalog already sells under another
	// name. The gate above only asks whether a tcgdex card joined a
	// product, and a join fails wherever the two sources cut a set
	// differently: tcgdex files a trainer kit as one set per mascot where
	// TCGplayer files the pair as one group, and files a Radiant Collection
	// inside its parent set where TCGplayer gives it a group of its own.
	// Every card of the unjoined half then became a card of its own - an
	// unpriced twin of a card already here, splitting one printing's
	// identity so a listing could land on either and only one carried a
	// price. 619 of 892 minted entries were that.
	//
	// The test is at the set level on purpose. A card-level one refuses
	// real cards: a McDonald's Pikachu and a Forbidden Light Pikachu can
	// share a name and a number and be different cards, and there are some
	// forty such coincidences. A whole tcgdex set whose cards are the cards
	// of one catalog group is not a coincidence - the shadowing sets run
	// 67% to 100% of their cards onto a single group, and the sets that
	// merely collide here and there run 6% to 33%, with nothing between.
	pricedIdentity := map[string][]string{}
	for _, entry := range cards {
		e, isMap := entry.(map[string]any)
		if !isMap {
			continue
		}
		name, _ := e["name"].(string)
		number, _ := e["number"].(string)
		setCode, _ := e["setCode"].(string)
		key := identityKey(name, number)
		pricedIdentity[key] = append(pricedIdentity[key], setCode)
	}

	// How much of each mintable set the catalog already sells, and where.
	type shadow struct {
		group string
		share float64
		cards int
	}
	printingsIn := map[string]map[string]bool{}
	twinsIn := map[string]map[string]int{}
	for _, card := range mintable {
		set := card.Set.ID
		if printingsIn[set] == nil {
			printingsIn[set] = map[string]bool{}
			twinsIn[set] = map[string]int{}
		}
		key := identityKey(card.Name, numberOf(card.LocalID))
		if printingsIn[set][key] {
			continue
		}
		printingsIn[set][key] = true
		for _, code := range pricedIdentity[key] {
			twinsIn[set][code]++
			break
		}
	}
	shadows := map[string]shadow{}
	for set, counts := range twinsIn {
		var best string
		var n int
		for code, c := range counts {
			if c > n || (c == n && code < best) {
				best, n = code, c
			}
		}
		total := len(printingsIn[set])
		if total == 0 || best == "" {
			continue
		}
		if share := float64(n) / float64(total); share >= shadowShare {
			shadows[set] = shadow{group: best, share: share, cards: total}
		}
	}
	var shadowed []string
	for set := range shadows {
		shadowed = append(shadowed, set)
	}
	sort.Strings(shadowed)
	var mintedTwins int
	for _, set := range shadowed {
		sh := shadows[set]
		log.Printf("minted: tcgdex set %q is %.0f%% the catalog's %q; its %d printings are not minted",
			set, 100*sh.share, sh.group, sh.cards)
	}

	// The numbers the catalog gives each name in a set, for the two cases
	// the identity above cannot see: a set the catalog numbers its own way,
	// and a name it sells without a number at all.
	pricedNumbers := map[string]map[string]bool{}
	for _, entry := range cards {
		e, isMap := entry.(map[string]any)
		if !isMap {
			continue
		}
		name, _ := e["name"].(string)
		number, _ := e["number"].(string)
		setCode, _ := e["setCode"].(string)
		key := setCode + "|" + identityKey(name, "")
		if pricedNumbers[key] == nil {
			pricedNumbers[key] = map[string]bool{}
		}
		pricedNumbers[key][number] = true
	}

	var renumberedTwins, unnumberedTwins int
	for _, card := range mintable {
		if _, shadowing := shadows[card.Set.ID]; shadowing {
			mintedTwins++
			continue
		}
		// The two cases the identity above cannot see, both of them a name
		// the catalog already sells in this set under a number that cannot
		// be compared. Anything else falls through and is minted: the
		// lettered alternate arts share a set and a name with the card they
		// alter - "Hex Maniac 75a" beside 75 - and are cards of their own.
		numbers := pricedNumbers[mintedSetCode[card.Set.ID]+"|"+identityKey(card.Name, "")]
		switch {
		// A set the catalog numbers its own way: the name is all there is
		// to go on, and the catalog selling that name here is the answer.
		case len(numbers) > 0 && renumberedSets[card.Set.ID]:
			renumberedTwins++
			continue
		// A name the catalog sells with no collector number at all. Nothing
		// can be told apart by number, so a name it already sells is the
		// same card. Only where no printing of that name carries one: a
		// name sold both ways - "Pikachu" in the promo drawers, at six
		// numbers and at none - says nothing either way.
		case len(numbers) == 1 && numbers[""]:
			unnumberedTwins++
			continue
		}
		var finishes []string
		for _, v := range []struct {
			flag   bool
			finish string
		}{
			{card.Variants.Normal, "Normal"},
			{card.Variants.Holo, "Holofoil"},
			{card.Variants.Reverse, "Reverse Holofoil"},
			// A plain "1st Edition" is the non-foil first printing, so it
			// exists only where a non-foil printing does. tcgdex flags the
			// two independently, and on a holo-only card the pair reads as
			// a non-foil first edition nobody ever printed - Machamp 8/102,
			// whose first edition is the holo the catalog already sells
			// under Deck Exclusives.
			{card.Variants.FirstEdition && card.Variants.Normal, "1st Edition"},
		} {
			if v.flag {
				finishes = append(finishes, v.finish)
			}
		}
		if len(finishes) == 0 {
			finishes = []string{"Normal"}
		}
		rarity := card.Rarity
		if rarity == "" {
			rarity = "None"
		}
		var image string
		if card.Image != "" {
			image = card.Image + "/high.webp"
		} else {
			mintedWithoutArt++
		}
		for _, finish := range finishes {
			entry := map[string]any{
				"id":       mintedIDBase(card.ID) + finishSuffixFor(finish),
				"name":     catalogSpelling(card.Name),
				"setCode":  mintedSetCode[card.Set.ID],
				"rarity":   rarity,
				"finish":   finish,
				"image":    image,
				"tcgdexId": card.ID,
			}
			if card.LocalID != "" {
				emitNumber(entry, numberOf(card.LocalID))
				own, _ := entry["number"].(string)
				if _, printed := entry["total"]; !printed && own != "" && own[0] >= '0' && own[0] <= '9' {
					if total, sole := totalBySet[mintedSetCode[card.Set.ID]]; sole {
						entry["total"] = total
						mintedTotals++
					}
				}
			}
			if card.Category != "" {
				entry["type"] = card.Category
			}
			cards = append(cards, entry)
			mintedCards++
		}
	}
	if mintedTwins > 0 {
		log.Printf("minted: %d tcgdex cards refused as twins of a product the catalog already sells", mintedTwins)
	}
	if renumberedTwins > 0 {
		log.Printf("minted: %d refused as the catalog's own cards under a numbering of their own", renumberedTwins)
	}
	if unnumberedTwins > 0 {
		log.Printf("minted: %d refused as names the catalog sells with no collector number to tell them apart", unnumberedTwins)
	}
	if mintedCards > 0 {
		log.Printf("minted: %d entries over %d tcgdex cards the catalog has no product for (%d of those cards have no art upstream, %d took the set's own printed total)",
			mintedCards, len(mintable), mintedWithoutArt, mintedTotals)
	}

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []any
	for _, product := range sealedProducts {
		code := setCodeFor(product)
		sealed = append(sealed, map[string]any{
			"id":          fmt.Sprintf("%s-%d", sanitizeID(code), product.ProductID),
			"name":        product.Name,
			"setCode":     code,
			"releaseDate": releaseDates[product.GroupID],
			"image":       imageURL(product.ImageURL),
			"externalLinks": map[string]any{
				"tcgPlayerId": product.ProductID,
			},
		})
	}
	// The set's own size, which is the total its cards print beside their
	// number. It is read back off the finished cards rather than from the
	// catalog, which publishes no size, so the minted entries count too;
	// a set whose cards name no single total carries none.
	var sized int
	for code, total := range totalsBySet(cards) {
		set, isMap := sets[code].(map[string]any)
		if !isMap {
			continue
		}
		size, err := strconv.Atoi(strings.TrimLeft(total, "0"))
		if err != nil || size <= 0 {
			continue
		}
		set["baseSetSize"] = size
		sized++
	}
	log.Printf("emitting %d sets, %d card entries over %d products, %d sealed (%d sets carry a printed size)",
		len(sets), len(cards), len(singles), len(sealed), sized)
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	// The id upstream knows this printing by, in the place every other
	// identifier lives. It has been written flat on the entry beside an
	// externalLinks holding only the TCGplayer id, so "which id spaces is
	// this card in" has been two questions rather than one - and three
	// across the eight games, because Riftbound writes the TCGplayer id
	// flat as well. It is written in both places for now: the loader reads
	// the flat one, and the flat one goes when it reads this one instead.
	var linked int
	for _, entry := range cards {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["tcgdexId"].(string)
		if id == "" {
			continue
		}
		links, ok := item["externalLinks"].(map[string]any)
		if !ok {
			links = map[string]any{}
			item["externalLinks"] = links
		}
		links["tcgdexId"] = id
		linked++
	}
	log.Printf("external links: %d cards carry their tcgdexId under externalLinks as well", linked)

	// The stamp programmes Cardmarket shelves wider than anyone else.
	var mkmMinted, mkmPassed int
	cards, mkmMinted, mkmPassed = mintFromCardmarket(*cardmarketCatalogPath, cards)
	log.Printf("cardmarket: minted %d stamped promos no other catalog lists, passed over %d whose stamped-from printing we do not carry",
		mkmMinted, mkmPassed)
	if mkmMinted == 0 {
		log.Fatalln("cardmarket: the catalog minted nothing; either the shelves were renamed or the file is not the Pokemon one")
	}

	doc := map[string]any{
		"game":   "pokemon",
		"sets":   sets,
		"cards":  cards,
		"sealed": sealed,
	}
	var buf bytes.Buffer
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	plainQuotes(doc)

	err = json.NewEncoder(&buf).Encode(doc)
	if err != nil {
		log.Fatalln(err)
	}

	// Re-read the encoded output and verify it structurally before
	// publishing anything: a format drift or a truncated download must
	// fail here, not in every consumer. The types mirror what go-mtgban's
	// loader reads, duplicated so this repository depends on nothing.
	counted, err := validate(buf.Bytes(), catalogFinishes)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d sealed", counted.sets, counted.cards, counted.sealed)
	if counted.cards != len(cards) || counted.sealed != len(sealed) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			len(cards), len(sealed), counted.cards, counted.sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a single, so it is exhaustive by
	// construction and cannot lose a product to a rule that did not know
	// what to do with it - the card side's whole failure mode. What it can
	// lose a product to is an edit: one `continue` on the sealed path and
	// the products would leave the datastore with nothing to say so, the
	// card side's invariant being blind to them. Counting the emitted
	// products back against the catalog total is what says so.
	wantSealed := len(catalog.Products) - len(singles)
	if counted.sealed != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			counted.sealed, wantSealed)
	}

	// Compare against the baseline, when the publish handed one over, and
	// say whether this build is fit to become the next one.
	fit := true
	if *against != "" || *baselineFit != "" {
		current, err := countDatastore(buf.Bytes())
		if err != nil {
			log.Fatalln("against:", err)
		}
		if *against != "" {
			previousData, err := os.ReadFile(*against)
			if err != nil {
				log.Fatalln("against:", err)
			}
			previous, err := countDatastore(previousData)
			if err != nil {
				log.Fatalln("against:", err)
			}
			log.Printf("against %s: %d cards (was %d), %d sealed (was %d), %d sets (was %d)",
				*against, current.cards, previous.cards, current.sealed, previous.sealed,
				len(current.bySet), len(previous.bySet))
			if err := regression(previous, current, *againstTolerance); err != nil {
				log.Fatalln("against: refusing to publish:", err)
			}
			// The baseline only ever moves forward. A build smaller than
			// it - legitimately, within the tolerance - must not become
			// the thing the next build is measured against, or a run of
			// tolerated drops ratchets it down one step at a time and the
			// whole loss is never large enough for any single run to see.
			// Measuring from the high-water mark instead means the drift
			// has to stay under the tolerance in total, not per night.
			fit = current.cards >= previous.cards && current.sealed >= previous.sealed
		}
		if *baselineFit != "" {
			if !fit {
				log.Print("baseline: unchanged, this build holds less than it does")
			} else {
				note := fmt.Sprintf("cards=%d sealed=%d\n", current.cards, current.sealed)
				if err := os.WriteFile(*baselineFit, []byte(note), 0o644); err != nil {
					log.Fatalln("baseline:", err)
				}
				log.Print("baseline: this build becomes the one the next is measured against")
			}
		}
	}

	out := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			log.Fatalln(err)
		}
		defer f.Close()
		out = f
	}
	_, err = out.Write(buf.Bytes())
	if err != nil {
		log.Fatalln(err)
	}
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

type counts struct {
	sets, cards, sealed int
}

// coverage is the zero-skip invariant: the products the emitted entries
// cover must be exactly the products the catalog types as cards. Checked on
// the encoded output, so a card product no rule above knew what to do with
// stops the publish instead of quietly leaving the datastore. The offender
// is named lowest id first, so the same data always reports the same one.
func coverage(got, want map[int][]string) error {
	var missing, extra []int
	for productID := range want {
		_, found := got[productID]
		if !found {
			missing = append(missing, productID)
		}
	}
	for productID := range got {
		_, found := want[productID]
		if !found {
			extra = append(extra, productID)
		}
	}
	sort.Ints(missing)
	sort.Ints(extra)
	if len(missing) > 0 {
		return fmt.Errorf("%d catalog card products carry no entry, first is %d",
			len(missing), missing[0])
	}
	if len(extra) > 0 {
		return fmt.Errorf("%d entries name a product the catalog does not type as a card, first is %d",
			len(extra), extra[0])
	}
	return nil
}

// validate decodes an encoded datastore and checks its shape: every card
// and sealed product carrying its identity, every id unique within its
// namespace, no two products wearing the same identity, every referenced set
// existing, every finish one of the seven printing names, and every product's
// entries covering exactly the sku printings the catalog lists for it.
// codeShape is what a set code has to look like to be asked for: a search
// query is split on whitespace before a filter sees it and on the colon that
// names the filter, so a code holding either can never be typed after "is:".
var codeShape = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// idShape is what a uuid has to look like wherever one is written down: a
// slash is a path separator and a space ends a word, and a uuid travels
// through urls, filenames and query strings alike.
var idShape = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validate(data []byte, wantFinishes map[int][]string) (counts, error) {
	var doc struct {
		Game string `json:"game"`
		Sets map[string]struct {
			Name string `json:"name"`
		} `json:"sets"`
		Cards []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Number        string `json:"number"`
			Total         string `json:"total"`
			SetCode       string `json:"setCode"`
			Variant       string `json:"variant"`
			Rarity        string `json:"rarity"`
			Finish        string `json:"finish"`
			Image         string `json:"image"`
			ExternalLinks struct {
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"cards"`
		Sealed []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"sealed"`
	}
	var out counts
	err := json.Unmarshal(data, &doc)
	if err != nil {
		return out, err
	}

	if doc.Game != "pokemon" {
		return out, fmt.Errorf("game is %q, not pokemon", doc.Game)
	}
	for code, set := range doc.Sets {
		if code == "" || set.Name == "" {
			return out, fmt.Errorf("set %q missing its identity", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cardIDs := map[string]bool{}
	// A query resolves a card by its name, number, set, variant label and
	// rarity, never by the id, so two products wearing all five alike are
	// one card to every consumer and would alias each other's prices. The
	// key holds the product id rather than a flag so a product's own
	// printing entries pass while two different products never do. This is
	// what the collision guard's restores are for: what it cannot tell
	// apart fails the build here instead of being published.
	// The discriminator two entries wearing one identity are told apart by:
	// the product for an entry that names one, and the card key for a
	// minted entry, which names no product because none exists. A minted
	// card's own finishes share that key and pass, exactly as a product's
	// sibling printings do.
	identities := map[string]string{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		// The rarity is part of the identity, but its presence is
		// TCGplayer's to provide, not this build's to demand: a freshly
		// listed product carries none for a day, and one card without a
		// rarity is no reason to publish nothing at all. The identity
		// check below still refuses two products indistinguishable
		// without it.
		if card.ID == "" || card.Name == "" || card.SetCode == "" ||
			card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		// An entry that names a product is one TCGplayer sells, and it
		// always has the catalog's image. A minted entry has whatever
		// tcgdex holds, which for some cards is no art at all.
		if card.Image == "" && card.ExternalLinks.TcgPlayerID != 0 {
			return out, fmt.Errorf("card %q (%s) carries no image", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.Contains(card.Number, "/") {
			return out, fmt.Errorf("card %q (%s) carries the set total inside its number: %q", card.Name, card.ID, card.Number)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q", card.Name, card.ID, card.Number)
		}
		if card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) carries no finish at all", card.Name, card.ID)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		// The total joins the identity because it is at times the only
		// thing telling two promos apart: the 1840 group sells the Cracked
		// Ice Kyogre of the 160-card set beside the 236-card set's, both
		// "053", and only the set size on the face says which is which.
		identity := strings.Join([]string{
			card.Name, card.Number, card.Total, card.SetCode, card.Variant, card.Rarity}, "|")
		productID := card.ExternalLinks.TcgPlayerID
		discriminator := fmt.Sprint(productID)
		if productID == 0 {
			discriminator = "minted:" + card.SetCode + "|" + card.Number
		}
		other, seen := identities[identity]
		if seen && other != discriminator {
			return out, fmt.Errorf("%s and %s wear one identity: %s",
				other, discriminator, identity)
		}
		identities[identity] = discriminator
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		// A minted entry counts for no product, so the coverage check below
		// still compares exactly the catalog's card products against the
		// entries that name one.
		if productID == 0 {
			continue
		}
		if sliceContains(gotFinishes[productID], card.Finish) {
			return out, fmt.Errorf("product %d carries finish %q twice", productID, card.Finish)
		}
		gotFinishes[productID] = append(gotFinishes[productID], card.Finish)
	}
	err = coverage(gotFinishes, wantFinishes)
	if err != nil {
		return out, err
	}
	for productID, want := range wantFinishes {
		got := append([]string(nil), gotFinishes[productID]...)
		sort.Strings(got)
		expected := append([]string(nil), want...)
		sort.Strings(expected)
		if strings.Join(got, "|") != strings.Join(expected, "|") {
			return out, fmt.Errorf("product %d emits finishes %v, skus carry %v", productID, got, expected)
		}
	}
	sealedIDs := map[string]bool{}
	for _, product := range doc.Sealed {
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerID == 0 {
			return out, fmt.Errorf("sealed %q (%s) missing identity", product.Name, product.ID)
		}
		if !idShape.MatchString(product.ID) {
			return out, fmt.Errorf("sealed %q has a uuid nothing can carry: %q", product.Name, product.ID)
		}
		if sealedIDs[product.ID] {
			return out, fmt.Errorf("duplicate sealed id %s", product.ID)
		}
		sealedIDs[product.ID] = true
		if _, found := doc.Sets[product.SetCode]; !found {
			return out, fmt.Errorf("sealed %q in unknown set %s", product.Name, product.SetCode)
		}
	}
	out.sets = len(doc.Sets)
	out.cards = len(doc.Cards)
	out.sealed = len(doc.Sealed)
	return out, nil
}

// typographic is the quotes a catalog spells with and no consumer queries
// with.
var typographic = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'", "\u201c", `"`, "\u201d", `"`)

// plainQuotes rewrites those quotes wherever the document carries them.
// The catalogs are not consistent about it: TCGplayer sells "Rocket's
// Hitmonchan" with a curly apostrophe beside hundreds of names holding a
// plain one, files two Yu-Gi-Oh rarities as "Ultra Pharaoh's Rare" while
// every other name uses ASCII, and spells one One Piece card
// Eustass"Captain"Kid on the card and Eustass"Captain"Kid on the box it
// comes in. A query carries one spelling, so the card filed under the
// other cannot be found, and the two rarities cannot be asked for at all.
//
// The whole document is walked rather than the fields known to carry
// them, because the field that starts carrying them tomorrow would
// otherwise be missed, and it runs before the output is encoded so the
// check that re-reads it sees exactly what will be published.
func plainQuotes(v any) any {
	switch t := v.(type) {
	case string:
		return typographic.Replace(t)
	case map[string]any:
		for k, e := range t {
			t[k] = plainQuotes(e)
		}
	case []any:
		for i, e := range t {
			t[i] = plainQuotes(e)
		}
	}
	return v
}

// finishSuffixFor is the id suffix a printing's entries carry: the pinned
// one above where this build knows the printing, and one spelled from the
// name where it does not. TCGplayer adds a printing to a category when it
// likes and is selling the skus either way, so a build that stopped instead
// would publish nothing at all rather than publish the new printing late.
//
// The pins are what keep an id still - they are the suffixes already in
// circulation. The one thing this cannot absorb is a pinned printing being
// renamed, which checkPinnedPrintings refuses.
func finishSuffixFor(name string) string {
	if suffix, known := finishSuffix[name]; known {
		return suffix
	}
	return "_" + finishSlug(name)
}

// finishSlug spells a printing name the way an id carries it.
func finishSlug(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// checkPinnedPrintings refuses a catalog that no longer lists a printing
// this build pins an id suffix for. A printing TCGplayer adds is absorbed
// above; one it renames is not and cannot be - every id built from the old
// name would move to the new one, silently, since an id nobody stored
// resolves to nothing rather than erroring.
func checkPinnedPrintings(c *tcgplayer.CatalogDump) {
	listed := map[string]bool{}
	for _, printing := range c.Printings {
		listed[printing.Name] = true
	}
	for name, suffix := range finishSuffix {
		if !listed[name] {
			log.Fatalf("the catalog no longer lists printing %q, which this build pins the id suffix %q for: every id built from it would move",
				name, suffix)
		}
	}
}
