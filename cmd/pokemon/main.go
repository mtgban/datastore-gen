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
// Number field; a number-like tail that disagrees comes off as well, since
// no card is named with a number, and is said out loud - the typo could be
// in either field. The catalog dump keeps its wording under the entry's
// tcgPlayerId.
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
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mtgban/datastore-gen/internal/baseline"
	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/handtable"
	"github.com/mtgban/datastore-gen/internal/validate"
	"github.com/mtgban/datastore-gen/internal/vocabulary"
	"github.com/mtgban/go-tcgplayer"
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
	tcgdexCardsQuery = "{ cards { id localId name rarity category types image variants { normal reverse holo firstEdition } set { id } } }"
)

// tcgSingles are the product types single cards are filed under, as the
// catalog names them for this game; the only other type in this category
// is Sealed Products.
var tcgSingles = tcgplayer.SinglesProductTypes(pokemonCategory)

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
	catalog, err := emit.ReadCatalog(*catalogPath, pokemonCategory)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}

	setsData, err := loadTcgdex(*tcgdexSets, *upstreamCache, "tcgdex-sets.json", tcgdexSetsQuery)
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
	setsResponse.Sets = emit.DropRepeats("tcgdex sets", setsResponse.Sets, func(s tcgdexSet) string { return s.ID })
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
	cardsData, err := loadTcgdex(*tcgdexCards, *upstreamCache, "tcgdex-cards.json", tcgdexCardsQuery)
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
	cardsResponse.Cards = emit.DropRepeats("tcgdex cards", cardsResponse.Cards, func(c tcgdexCard) string { return c.ID })

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
		code := emit.SetCodeOf(group.Abbreviation)
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
		code = emit.FreeSetCode(usedCodes, code)
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
		alias, aliased := setAliases.Get(name)
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
	var placeholders, filled int
	for _, group := range groups {
		if productsIn[group.GroupID] == 0 {
			continue
		}
		if emit.HasDate(group) {
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
		if date, found := handDates.Get(group.GroupID); found {
			releaseDates[group.GroupID] = date
			filled++
			log.Printf("%s (%s): release date %s filled by hand",
				group.Name, setCodes[group.GroupID], date)
			continue
		}
		log.Printf("%s (%s): no release date anywhere, left undated", group.Name, setCodes[group.GroupID])
	}
	log.Printf("release dates: %d placeholders, %d filled, %d left empty",
		placeholders, filled, placeholders-filled)

	printings := printingNames(&catalog)
	if strays := dropStrayPrintRuns(&catalog, printings); strays > 0 {
		log.Printf("print run: %d stray run printings dropped", strays)
	}

	// Split the products: every single becomes card entries per sku
	// printing, Sealed Products become sealed. "N/A" is a spelling of no
	// number; the unnumbered are real singles and stay.
	var singles []single
	var numberLedQuals int
	var sealedProducts []tcgplayer.Product
	var codeCards, unnumbered int
	var unpriced []string
	for _, product := range catalog.Products {
		if !slices.Contains(tcgSingles, product.ProductType) {
			sealedProducts = append(sealedProducts, product)
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			// Nothing English is sold for it yet: no printing to carry until there is.
			unpriced = append(unpriced, fmt.Sprintf("%q (%d)", product.Name, product.ProductID))
			continue
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
	if len(unpriced) > 0 {
		log.Printf("unpriced: %d card products TCGplayer sells no English sku for yet, carried once it does: %s",
			len(unpriced), strings.Join(unpriced, ", "))
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
			if droppedQuals.Has(strings.ToLower(q.text)) || isBareLetter(q.text) {
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
			if fixed, hand := qualSpellings.Get(strings.ToLower(q.text)); hand {
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
			if m != nil && variantOnlyQuals.Has(strings.ToLower(m[1])) {
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
	cardTypes := &printedTypes{}
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
		hasNormal := slices.Contains(names, "Normal") ||
			slices.Contains(names, "1st Edition") || slices.Contains(names, "Unlimited")
		hasHolo := slices.Contains(names, "Holofoil") ||
			slices.Contains(names, "1st Edition Holofoil") || slices.Contains(names, "Unlimited Holofoil")
		tracked := setAxes[card.Set.ID]
		checks := []struct {
			axis    string
			tcgdex  bool
			tracked bool
			printed bool
		}{
			{"normal", card.Variants.Normal, tracked.normal, hasNormal},
			{"reverse", card.Variants.Reverse, tracked.reverse, slices.Contains(names, "Reverse Holofoil")},
			{"holo", card.Variants.Holo, tracked.holo, hasHolo},
			{"firstEdition", card.Variants.FirstEdition, tracked.firstEdition,
				slices.Contains(names, "1st Edition") || slices.Contains(names, "1st Edition Holofoil")},
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
	sets := map[string]*emit.Set{}
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
		set := &emit.Set{
			Name:         group.Name,
			ReleaseDate:  releaseDates[group.GroupID],
			Abbreviation: group.Abbreviation,
		}
		// The mark the set's cards print, from the tcgdex set the group
		// joined. A group that joined no set, or joined one tcgdex holds no
		// symbol for, carries none, and whatever renders this falls back to
		// drawing the set code.
		if dex := joinedSets[group.GroupID]; dex != nil && dex.Symbol != "" {
			set.Symbol = tcgdexSymbolURL(dex.Symbol)
			symboled++
		}
		// The type is what tells the matcher a printing is promotional, so
		// only the wholly promotional groups carry it.
		if promoted[group.GroupID] {
			set.Type = "promo"
			promoSets++
		}
		sets[setCodes[group.GroupID]] = set
	}
	for year, code := range worldsSets {
		sets[code] = &emit.Set{
			Name:        year + " World Championship Decks",
			ReleaseDate: worldsReleaseDate(year),
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
	// The sets minted here, so the ones the twin refusals below empty can
	// be taken back out: a set is minted for every set a mintable card sits
	// in, and a wholly shadowed set - a trainer kit whose every card is one
	// catalog group's - ends up published with no card in it.
	mintedSetEntries := map[string]bool{}
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
		code := emit.SetCodeOf(card.Set.ID)
		if code == "" {
			log.Fatalf("tcgdex set %q reduces to no set code", card.Set.ID)
		}
		if usedCodes[code] {
			code = code + "-DEX"
			log.Printf("tcgdex set %s: code already taken, minted set code %s", card.Set.ID, code)
		}
		code = emit.FreeSetCode(usedCodes, code)
		usedCodes[code] = true
		mintedSetCode[card.Set.ID] = code
		set := &emit.Set{Name: card.Set.ID}
		if dex != nil {
			set.Name = dex.Name
			set.ReleaseDate = dex.ReleaseDate
			if dex.Symbol != "" {
				set.Symbol = tcgdexSymbolURL(dex.Symbol)
				symboled++
			}
		}
		sets[code] = set
		mintedSetEntries[code] = true
		mintedSets++
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
	ptcg, ptcgErr := loadPokemontcgSets(*pokemontcgSets, *upstreamCache)
	if ptcgErr != nil {
		log.Printf("pokemontcg.io: %v; the sets tcgdex has no symbol for keep none", ptcgErr)
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
		for code, set := range sets {
			if set.Symbol != "" {
				continue
			}
			name, date := set.Name, set.ReleaseDate
			if found, ok := byName[mtgmatcherNormalize(name)]; ok {
				set.Symbol = found.Images.Symbol
				byNameHits++
				symboled++
				continue
			}
			if found, ok := byCodeDate[strings.ToUpper(code)+"|"+date]; ok {
				set.Symbol = found.Images.Symbol
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
		if len(printings[product.ProductID]) == 0 {
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
	// stays the variant it already was. Scoped to the shelves because the
	// same test unscoped reads "Gym Challenge" as the 2000 set rather than
	// the tournament tier the promo shelves name.
	//
	// The other shelves are named in shelfNames: the jumbo cards, the
	// Burger King and World Collection promos and the miscellany, each of
	// which holds a reprint from anywhere and writes where from beside it
	// - "Hoopa EX (Ancient Origins)" at 036/098, "Chimchar [Diamond &
	// Pearl]" at 076/130.
	facts := published{
		pokemon:  pokemon,
		setNames: map[string]bool{},
		tails:    map[string]bool{},
	}
	// The spellings a set is written under: the name whole; without its
	// era, because a label writes the set either way ("Ruby & Sapphire"
	// for our "EX Ruby and Sapphire"); without its code, which this
	// datastore writes in front with a colon and a label never writes at
	// all ("Twilight Masquerade Stamped" for our "SV06: Twilight
	// Masquerade"); and as tcgdex spells it, which is how the catalog
	// writes a promo set on a jumbo card ("Charizard GX (SM Black Star
	// Promos)" for a reprint out of our "SM Promos"), a promo set being its
	// family too ("Bewear GX (Black Star Promos)").
	//
	// A set's own spellings are kept by code as well, for a label that
	// names the set the card is already in - the whole name, the tcgdex
	// name and the family, and not the era-less one, which is a card kind
	// as often as a set: "Delta Species" inside EX Delta Species says which
	// of its cards are the delta ones.
	own := map[string]map[string]bool{}
	shelf := map[string]bool{}
	for code, set := range sets {
		name := set.Name
		if name == "" {
			continue
		}
		whole := []string{name}
		if alias, aliased := setAliases.Get(name); aliased {
			whole = append(whole, alias)
			if strings.HasSuffix(alias, "Black Star Promos") {
				whole = append(whole, "Black Star Promos")
			}
		}
		spellings := slices.Clone(whole)
		if _, rest := eraOf(name); rest != "" {
			spellings = append(spellings, rest)
		}
		if _, rest, found := strings.Cut(name, ": "); found && rest != "" {
			spellings = append(spellings, rest)
		}
		own[code] = map[string]bool{}
		for _, spelling := range whole {
			own[code][mtgmatcherNormalize(spelling)] = true
		}
		for _, spelling := range spellings {
			facts.setNames[mtgmatcherNormalize(spelling)] = true
		}
		if strings.HasSuffix(name, "Exclusives") || shelfNames.Has(name) {
			shelf[code] = true
		}
	}
	setNames := facts.setNames
	// The labels a promotion is written as on its own: every label the
	// catalog writes, and whatever a label too long for a token leaves
	// behind once the set or Pokemon it opens with comes off.
	for label := range written {
		facts.tails[label] = true
	}
	for i := range singles {
		for _, q := range singles[i].quals {
			if len(emit.PromoSlug(q.text)) <= promoTypeLimit {
				continue
			}
			if _, rest := publishedPrefix(q.text, pokemon, setNames); rest != "" {
				facts.tails[rest] = true
			}
		}
	}

	var cards []*emit.Card
	// What promoTypesOf left out of each entry, keyed by the entry's id,
	// for markPrintings to put back where it turns out to be doing the
	// distinguishing.
	leftOut := map[string][]string{}
	// The year each entry's labels stated, keyed by the entry's id, for
	// datePrintings to publish where the set does not already state it.
	stated := map[string]string{}
	for i := range singles {
		s := &singles[i]
		productID := s.product.ProductID

		image := emit.ImageURL(s.product.ImageURL)
		dex := dexCards[productID]
		if dex != nil && dex.Image != "" {
			image = dex.Image + "/high.webp"
		}
		for _, finish := range printings[productID] {
			suffix := emit.FinishSuffix(finish)
			cardType := s.product.Extended("Card Type")
			mark := printMark(s, setCodeFor(s.product))
			entry := &emit.Card{
				ID:            idBase(s.number, productID) + suffix,
				Name:          handName(s.product.ProductID, s.baseName),
				SetCode:       setCodeFor(s.product),
				Rarity:        s.product.Extended("Rarity"),
				Finish:        finish,
				Watermark:     mark,
				Type:          cardType,
				Types:         cardTypes.of(dex, cardType),
				Image:         image,
				ExternalLinks: emit.Links{TCGPlayerID: productID},
			}
			if dex != nil {
				entry.ExternalLinks.TcgdexID = dex.ID
			}
			if s.number != "" {
				emitNumber(entry, emit.QueryNumber(s.number))
			}
			variant := variantOf(s)
			if variant != "" {
				entry.Variant = variant
				// The same labels as a list: joined, "Full Art Staff"
				// cannot be read back into the two tags it holds, and the
				// matcher needs them whole to declare and to match on.
				// A printing whose only label was a Pokemon has a variant
				// and no promo types, so the key stays off rather than
				// carrying an empty list.
				tags, left, year, found, spoken := promoTypesOf(s, facts, shelf[setCodeFor(s.product)], own[setCodeFor(s.product)], finish, mark)
				entry.Language = spoken
				if year != "" {
					stated[entry.ID] = year
				}
				if mark == "" && found != "" {
					entry.Watermark = found
				}
				// A label saying what the finish field says names no
				// promotion: the catalog writes "Reverse Holofoil" into
				// the product name of a printing it also prices as one,
				// and as a tag that declares a printing promotional for
				// being the finish it is.
				tags = slices.DeleteFunc(tags, func(tag string) bool {
					return saysFinish(tag, finish)
				})
				entry.PromoTypes = tags
				if len(left) > 0 {
					leftOut[entry.ID] = left
				}
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
	// Read before anything is minted, so the widths are the catalog's own.
	widthBySet := numberWidthsBySet(cards)

	var mintedCards, mintedWithoutArt int
	var mintedTotals, mintedPadded int
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
	for _, e := range cards {
		key := identityKey(e.Name, e.Number)
		pricedIdentity[key] = append(pricedIdentity[key], e.SetCode)
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
		key := identityKey(card.Name, emit.QueryNumber(card.LocalID))
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
	for _, e := range cards {
		key := e.SetCode + "|" + identityKey(e.Name, "")
		if pricedNumbers[key] == nil {
			pricedNumbers[key] = map[string]bool{}
		}
		pricedNumbers[key][e.Number] = true
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
		case len(numbers) > 0 && renumberedSets.Has(card.Set.ID):
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
			entry := &emit.Card{
				ID:            mintedIDBase(card.ID) + emit.FinishSuffix(finish),
				Name:          catalogSpelling(card.Name),
				SetCode:       mintedSetCode[card.Set.ID],
				Rarity:        rarity,
				Finish:        finish,
				Type:          card.Category,
				Types:         cardTypes.of(card, ""),
				Image:         image,
				ExternalLinks: emit.Links{TcgdexID: card.ID},
			}
			if card.LocalID != "" {
				emitNumber(entry, emit.QueryNumber(card.LocalID))
				own := entry.Number
				if width, agreed := widthBySet[mintedSetCode[card.Set.ID]]; agreed &&
					isAllDigits(own) && len(own) < width {
					padded := strings.Repeat("0", width-len(own)) + own
					entry.Number = padded
					own = padded
					mintedPadded++
					log.Printf("minted: %s numbers %q where %s pads to %d; published as %q",
						card.ID, card.LocalID, mintedSetCode[card.Set.ID], width, padded)
				}
				if entry.Total == "" && own != "" && own[0] >= '0' && own[0] <= '9' {
					if total, sole := totalBySet[mintedSetCode[card.Set.ID]]; sole {
						entry.Total = total
						mintedTotals++
					}
				}
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
		log.Printf("minted: %d entries over %d tcgdex cards the catalog has no product for (%d of those cards have no art upstream, %d took the set's own printed total, %d took the set's own number width)",
			mintedCards, len(mintable), mintedWithoutArt, mintedTotals, mintedPadded)
	}
	cardsIn := map[string]int{}
	for _, e := range cards {
		cardsIn[e.SetCode]++
	}
	var emptied int
	for code := range mintedSetEntries {
		if cardsIn[code] == 0 {
			delete(sets, code)
			emptied++
		}
	}
	if mintedSets > 0 {
		log.Printf("sets minted for tcgdex sets no group joined: %d, of which %d held no card once the twins were refused and are not published",
			mintedSets, emptied)
	}

	sort.Slice(sealedProducts, func(i, j int) bool {
		return sealedProducts[i].ProductID < sealedProducts[j].ProductID
	})
	var sealed []*emit.Sealed
	for _, product := range sealedProducts {
		code := setCodeFor(product)
		sealed = append(sealed, &emit.Sealed{
			ID:            fmt.Sprintf("%s-%d", sanitizeID(code), product.ProductID),
			Name:          product.Name,
			SetCode:       code,
			ReleaseDate:   releaseDates[product.GroupID],
			Image:         emit.ImageURL(product.ImageURL),
			ExternalLinks: emit.Links{TCGPlayerID: product.ProductID},
		})
	}
	// The set's own size, which is the total its cards print beside their
	// number. It is read back off the finished cards rather than from the
	// catalog, which publishes no size, so the minted entries count too;
	// a set whose cards name no single total carries none.
	var sized int
	for code, total := range totalsBySet(cards) {
		set, held := sets[code]
		if !held {
			continue
		}
		size, err := strconv.Atoi(strings.TrimLeft(total, "0"))
		if err != nil || size <= 0 {
			continue
		}
		set.BaseSetSize = size
		sized++
	}
	log.Printf("emitting %d sets, %d card entries over %d products, %d sealed (%d sets carry a printed size)",
		len(sets), len(cards), len(singles), len(sealed), sized)
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		len(singles), len(catalogFinishes), len(catalogFinishes)-len(singles))

	var linked int
	for _, entry := range cards {
		if entry.ExternalLinks.TcgdexID != "" {
			linked++
		}
	}
	log.Printf("external links: %d cards carry their tcgdexId under externalLinks", linked)

	// The stamp programmes Cardmarket shelves wider than anyone else.
	var mkmMinted, mkmPassed int
	cards, mkmMinted, mkmPassed = mintFromCardmarket(*cardmarketCatalogPath, cards, sets, ptcg)
	log.Printf("cardmarket: minted %d stamped promos no other catalog lists, passed over %d whose stamped-from printing we do not carry",
		mkmMinted, mkmPassed)
	if mkmMinted == 0 {
		log.Fatalln("cardmarket: the catalog minted nothing; either the shelves were renamed or the file is not the Pokemon one")
	}

	log.Printf("release dates: %d printings dated by a year their label stated, where the set states another",
		datePrintings(cards, sets, stated))
	marked, alike := markPrintings(cards, leftOut)

	log.Printf("watermarks: %d printings marked by which copy of the number they are, %d still alike", marked, alike)

	doc := &emit.Datastore{
		Game:       "pokemon",
		Properties: properties,
		Sets:       sets,
		Cards:      cards,
		Sealed:     sealed,
	}
	doc.DropRepeatedFinishes()
	doc.DropEmptySets()
	var buf bytes.Buffer
	log.Printf("types: %d printings typed by tcgdex, %d by the catalog's Card Type",
		cardTypes.fromTcgdex, cardTypes.fromCatalog)
	handtable.Report()
	// Spell the quotes the way a query does before anything reads the
	// document, so the check below sees what will be published.
	doc.PlainQuotes()

	envelope := emit.Envelope(emit.Today(), doc)
	err = json.NewEncoder(&buf).Encode(envelope)
	if err != nil {
		log.Fatalln(err)
	}

	// A token the vocabulary refuses comes off its printing here, logged,
	// rather than stopping every card of the game at the publish's check.
	{
		setAside, err := vocabulary.FitInto(&buf)
		if err != nil {
			log.Fatalln(err)
		}
		for _, line := range setAside {
			log.Println("vocabulary:", line)
		}
	}

	// Re-read the encoded output and verify it structurally before
	// publishing anything: a format drift or a truncated download must
	// fail here, not in every consumer. The types mirror what go-mtgban's
	// loader reads, duplicated so this repository depends on nothing.
	counted, err := validate.Datastore(buf.Bytes(), catalogFinishes, validationRules())
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d sealed", counted.Sets, counted.Cards, counted.Sealed)
	if counted.Cards != len(doc.Cards) || counted.Sealed != len(doc.Sealed) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			len(doc.Cards), len(doc.Sealed), counted.Cards, counted.Sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a single, so it is exhaustive by
	// construction and cannot lose a product to a rule that did not know
	// what to do with it - the card side's whole failure mode. What it can
	// lose a product to is an edit: one `continue` on the sealed path and
	// the products would leave the datastore with nothing to say so, the
	// card side's invariant being blind to them. Counting the emitted
	// products back against the catalog total is what says so.
	wantSealed := len(catalog.Products) - len(singles) - len(unpriced)
	if counted.Sealed != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			counted.Sealed, wantSealed)
	}

	// Compare against the baseline, when the publish handed one over, and
	// say whether this build is fit to become the next one.
	if err := baseline.Guard(buf.Bytes(), baseline.Count, baseline.Options{
		Against: *against, Tolerance: *againstTolerance, FitPath: *baselineFit,
	}); err != nil {
		log.Fatalln(err)
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
