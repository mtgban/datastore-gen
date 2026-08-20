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
	"flag"
	"fmt"
	"github.com/mtgban/go-tcgplayer"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	pokemonCategory = 3

	// tcgSingles is the product type single cards are filed under; the
	// only other type in this category is Sealed Products.
	tcgSingles = "Cards"

	// codeCardRarity marks the digital code cards, counted apart because
	// they are the one population here that is not a playable card.
	codeCardRarity = "Code Card"

	tcgdexGraphQLURL = "https://api.tcgdex.net/v2/graphql"

	// tcgpSerie is tcgdex's digital Pocket serie, dropped before joining.
	tcgpSerie = "tcgp"

	tcgdexSetsQuery  = "{ sets { id name releaseDate serie { id } } }"
	tcgdexCardsQuery = "{ cards { id localId image variants { normal reverse holo firstEdition } set { id } } }"
)

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

// finishOrder fixes the order a product's entries are emitted in.
var finishOrder = []string{
	"Normal",
	"Holofoil",
	"Reverse Holofoil",
	"1st Edition",
	"1st Edition Holofoil",
	"Unlimited",
	"Unlimited Holofoil",
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
// skus carry, in finishOrder; a printing the catalog does not list for a
// product is one that does not exist.
func printingNames(c *tcgplayer.CatalogDump) map[int][]string {
	name := map[int]string{}
	for _, p := range c.Printings {
		name[p.PrintingID] = p.Name
	}

	rank := map[string]int{}
	for i, n := range finishOrder {
		rank[n] = i
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
		sort.Slice(names, func(i, j int) bool {
			ri, iKnown := rank[names[i]]
			rj, jKnown := rank[names[j]]
			if iKnown && jKnown {
				return ri < rj
			}
			if iKnown != jKnown {
				return iKnown
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
	Serie       struct {
		ID string `json:"id"`
	} `json:"serie"`
}

// tcgdexCard is the slice of a tcgdex card this build reads.
type tcgdexCard struct {
	ID       string `json:"id"`
	LocalID  string `json:"localId"`
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

// loadTcgdex reads a raw GraphQL response envelope from a file when a path
// is given, or POSTs the query to the live endpoint.
func loadTcgdex(path, query string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(tcgdexGraphQLURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", tcgdexGraphQLURL, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
		return fmt.Errorf("graphql response carries no data")
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
// against tcgdex's "Black Star Promos" family, and the flagship base sets.
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
func componentEq(a, b string) bool {
	a = strings.TrimLeft(strings.ToUpper(strings.TrimSpace(a)), "0")
	b = strings.TrimLeft(strings.ToUpper(strings.TrimSpace(b)), "0")
	return a == b
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
		if product.ProductType != tcgSingles {
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
func promoTypesOf(s *single) []string {
	out := make([]string, 0, len(s.quals))
	for _, q := range s.quals {
		out = append(out, strings.ToLower(q.text))
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

// peelQuals peels the trailing parenthetical and bracket qualifiers off a
// name, outermost last, preserving their order and dropping a repeat of a
// qualifier the product already carries.
func peelQuals(name string) (string, []qual) {
	var quals []qual
	for {
		var q qual
		if m := parenTailRe.FindStringSubmatch(name); m != nil {
			q = qual{text: strings.TrimSpace(m[1])}
			name = strings.TrimSuffix(name, m[0])
		} else if m := bracketTailRe.FindStringSubmatch(name); m != nil {
			q = qual{text: strings.TrimSpace(m[1]), bracket: true}
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
func decompose(p tcgplayer.Product, num string) single {
	base, quals := peelQuals(p.Name)

	idx := strings.LastIndex(base, " - ")
	if idx >= 0 {
		tail := strings.TrimSpace(base[idx+3:])
		if restatesNumber(tail, num) {
			base = strings.TrimSpace(base[:idx])
		} else if numberLikeRe.MatchString(tail) {
			log.Printf("dash number: %q keeps tail %q, Number is %q", p.Name, tail, num)
		}
	}

	rarity := p.Extended("Rarity")
	s := single{product: p, number: num, baseName: base}
	for _, q := range quals {
		if restatesNumber(strings.TrimPrefix(q.text, "#"), num) || restatesRarity(q.text, rarity) {
			s.dropped = append(s.dropped, q)
			continue
		}
		s.quals = append(s.quals, q)
	}
	return s
}

// electionKey identifies the (group, number) bucket a name-versus-variant
// call is made in.
func electionKey(s *single) string {
	return fmt.Sprintf("%d|%s", s.product.GroupID, s.number)
}

// nonCodeRe matches the runs a set code cannot carry.
var nonCodeRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// setCodeOf reduces a catalog abbreviation to what a search query can carry.
// A set code is typed after "is:", and a query is split on whitespace before
// a filter ever sees it and on the colon that names the filter, so a code
// holding either cannot be asked for: "is:OP11 RE" reaches the filter as
// "is:OP11" and "is:crz:gg" names a filter called crz. Every run of anything
// but a letter or a digit becomes one dash, and the ends are trimmed of them.
func setCodeOf(abbreviation string) string {
	return strings.Trim(nonCodeRe.ReplaceAllString(abbreviation, "-"), "-")
}

func main() {
	output := flag.String("o", "", "output file (default stdout)")
	minCards := flag.Int("min-cards", 30000, "refuse to emit a datastore with fewer card entries")
	catalogPath := flag.String("tcg-catalog", "", "tcgdumper catalog dump for category 3 (required)")
	tcgdexSets := flag.String("tcgdex-sets", "", "tcgdex sets GraphQL response file (default: query the live API)")
	tcgdexCards := flag.String("tcgdex-cards", "", "tcgdex cards GraphQL response file (default: query the live API)")
	flag.Parse()

	if *catalogPath == "" {
		log.Fatalln("-tcg-catalog is required: the dump carries the printings and the ids")
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

	setsData, err := loadTcgdex(*tcgdexSets, tcgdexSetsQuery)
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
	cardsData, err := loadTcgdex(*tcgdexCards, tcgdexCardsQuery)
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
	for _, set := range setsResponse.Sets {
		if set.Serie.ID == tcgpSerie {
			continue
		}
		dexSets = append(dexSets, set)
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
		code := strings.ToLower(setCodeOf(group.Abbreviation))
		if code == "" {
			code = fmt.Sprintf("g%d", group.GroupID)
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
	releaseDates := map[int]string{}
	var placeholders, filled int
	for _, group := range groups {
		if hasDate(group) {
			releaseDates[group.GroupID] = group.ReleaseDate()
			continue
		}
		placeholders++
		dex := joinedSets[group.GroupID]
		if dex != nil && dex.ReleaseDate != "" {
			releaseDates[group.GroupID] = dex.ReleaseDate
			filled++
			log.Printf("%s (%s): release date %s filled from tcgdex",
				group.Name, setCodes[group.GroupID], dex.ReleaseDate)
			continue
		}
		log.Printf("%s (%s): no release date anywhere, left undated", group.Name, setCodes[group.GroupID])
	}
	log.Printf("release dates: %d placeholders, %d filled from tcgdex, %d left empty",
		placeholders, filled, placeholders-filled)

	printings := printingNames(&catalog)

	// Split the products: every single becomes card entries per sku
	// printing, Sealed Products become sealed. "N/A" is a spelling of no
	// number; the unnumbered are real singles and stay.
	var singles []single
	var sealedProducts []tcgplayer.Product
	var codeCards, unnumbered int
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
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
		singles = append(singles, decompose(product, num))
	}
	log.Printf("singles: %d kept (%d unnumbered, %d code cards), %d sealed",
		len(singles), unnumbered, codeCards, len(sealedProducts))
	if len(singles) == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}

	// Per collector number within its group: a qualifier every product of
	// the number carries is part of the name, not a variant. A number with
	// a single product cannot make that call alone, so the name parts
	// learned from the multi-product numbers decide for it — and for the
	// unnumbered singles, which have no bucket at all.
	byNumber := map[string][]*single{}
	for i := range singles {
		if singles[i].number == "" {
			continue
		}
		byNumber[electionKey(&singles[i])] = append(byNumber[electionKey(&singles[i])], &singles[i])
	}
	// A bucket may only make the all-carry call when it is one card: the
	// trainer kits file two different cards under one number, and a
	// decoration both happen to wear must stay a variant, not become part
	// of two names.
	sameBase := func(bucket []*single) bool {
		for _, s := range bucket[1:] {
			if s.baseName != bucket[0].baseName {
				return false
			}
		}
		return true
	}
	nameParens := map[string]bool{}
	for _, bucket := range byNumber {
		sort.Slice(bucket, func(i, j int) bool {
			return bucket[i].product.ProductID < bucket[j].product.ProductID
		})
		if len(bucket) < 2 || !sameBase(bucket) {
			continue
		}
		common := map[string]int{}
		for _, s := range bucket {
			for _, q := range s.quals {
				common[q.text]++
			}
		}
		for q, n := range common {
			if n == len(bucket) {
				nameParens[q] = true
			}
		}
	}
	assemble := func(s *single, isName func(string) bool) {
		name := []string{s.baseName}
		var variant []qual
		for _, q := range s.quals {
			if isName(q.text) {
				name = append(name, q.String())
			} else {
				variant = append(variant, q)
			}
		}
		s.baseName = strings.Join(name, " ")
		s.quals = variant
	}
	for _, bucket := range byNumber {
		// Decide before mutating: the membership test must read every
		// product's original qualifiers, not the ones a fold already moved.
		if len(bucket) < 2 {
			assemble(bucket[0], func(q string) bool { return nameParens[q] })
			continue
		}
		if !sameBase(bucket) {
			// Different cards under one number elect nothing at all:
			// whatever tells them apart is variant.
			for _, s := range bucket {
				assemble(s, func(q string) bool { return false })
			}
			continue
		}
		common := map[string]int{}
		for _, s := range bucket {
			for _, q := range s.quals {
				common[q.text]++
			}
		}
		for _, s := range bucket {
			assemble(s, func(q string) bool { return common[q] == len(bucket) })
		}
	}
	for i := range singles {
		if singles[i].number != "" {
			continue
		}
		assemble(&singles[i], func(q string) bool { return nameParens[q] })
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
					len(colliding), setCodes[s.product.GroupID], s.number, s.product.Name)
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
	sets := map[string]any{}
	promoted := promoGroups(catalog)
	var promoSets int
	for _, group := range groups {
		set := map[string]any{
			"name":        group.Name,
			"releaseDate": releaseDates[group.GroupID],
		}
		if group.Abbreviation != "" {
			set["abbreviation"] = group.Abbreviation
		}
		// The type is what tells the matcher a printing is promotional, so
		// only the wholly promotional groups carry it.
		if promoted[group.GroupID] {
			set["type"] = "promo"
			promoSets++
		}
		sets[setCodes[group.GroupID]] = set
	}
	log.Printf("promotional sets: %d of %d", promoSets, len(groups))

	// The coverage contract: every product the catalog types as a card,
	// with the sku printings it is sold in. validate reads it back off the
	// encoded output, so a product no rule here carried fails the build
	// instead of quietly leaving the datastore.
	catalogFinishes := map[int][]string{}
	for _, product := range catalog.Products {
		if product.ProductType != tcgSingles {
			continue
		}
		catalogFinishes[product.ProductID] = printings[product.ProductID]
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
			suffix, known := finishSuffix[finish]
			if !known {
				log.Fatalf("product %d carries printing %q, not one of the seven this identity scheme knows",
					productID, finish)
			}
			entry := map[string]any{
				"id":      idBase(s.number, productID) + suffix,
				"name":    s.baseName,
				"setCode": setCodes[s.product.GroupID],
				"rarity":  s.product.Extended("Rarity"),
				"finish":  finish,
				"image":   image,
				"externalLinks": map[string]any{
					"tcgPlayerId": productID,
				},
			}
			if s.number != "" {
				entry["number"] = numberOf(s.number)
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
				entry["promoTypes"] = promoTypesOf(s)
			}
			if dex != nil {
				entry["tcgdexId"] = dex.ID
			}
			cards = append(cards, entry)
		}
	}

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []any
	for _, product := range sealedProducts {
		code := setCodes[product.GroupID]
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
	log.Printf("emitting %d sets, %d card entries over %d products, %d sealed",
		len(sets), len(cards), len(singles), len(sealed))
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	doc := map[string]any{
		"game":   "pokemon",
		"sets":   sets,
		"cards":  cards,
		"sealed": sealed,
	}
	var buf bytes.Buffer
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
	if counted.cards < *minCards {
		log.Fatalf("only %d cards (minimum %d); refusing to publish", counted.cards, *minCards)
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
			SetCode       string `json:"setCode"`
			Variant       string `json:"variant"`
			Rarity        string `json:"rarity"`
			Finish        string `json:"finish"`
			Image         string `json:"image"`
			ExternalLinks struct {
				TcgPlayerId int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"cards"`
		Sealed []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerId int `json:"tcgPlayerId"`
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
	identities := map[string]int{}
	gotFinishes := map[int][]string{}
	for _, card := range doc.Cards {
		if card.ID == "" || card.Name == "" || card.SetCode == "" || card.Rarity == "" ||
			card.Finish == "" || card.Image == "" || card.ExternalLinks.TcgPlayerId == 0 {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q", card.Name, card.ID, card.Number)
		}
		if _, known := finishSuffix[card.Finish]; !known {
			return out, fmt.Errorf("card %q (%s) carries unknown finish %q", card.Name, card.ID, card.Finish)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		identity := strings.Join([]string{
			card.Name, card.Number, card.SetCode, card.Variant, card.Rarity}, "|")
		other, seen := identities[identity]
		if seen && other != card.ExternalLinks.TcgPlayerId {
			return out, fmt.Errorf("products %d and %d wear one identity: %s",
				other, card.ExternalLinks.TcgPlayerId, identity)
		}
		identities[identity] = card.ExternalLinks.TcgPlayerId
		if _, found := doc.Sets[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		productID := card.ExternalLinks.TcgPlayerId
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
		if product.ID == "" || product.Name == "" || product.ExternalLinks.TcgPlayerId == 0 {
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
