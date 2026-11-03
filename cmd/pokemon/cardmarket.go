package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/handtable"
	"github.com/mtgban/go-cardmarket"
)

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
var cardmarketMintSets = handtable.New("cardmarketMintSets", map[string]string{
	"Southeast Asia Promos": "SEA",
	"Professor Program":     "PPP",
})

// cardmarketSourceNumber splits the number Cardmarket writes on a stamped
// promo into the set it was reprinted from and the number it kept.
var cardmarketSourceNumber = regexp.MustCompile(`^([A-Za-z0-9-]{2,6}) *([0-9]+[a-z]?)$`)

// stampedName is a card's name as the two sides of the stamped-promo join
// can agree on it. Cardmarket writes a Supporter's version after a dash
// ("Professor's Research - Professor Sada") where this datastore keeps the
// name and puts the person in the mark, and it writes the basic energies
// "Basic Grass Energy" where TCGplayer's Professor Program products say
// "Grass Energy"; accents and case go the way every other name comparison
// here folds them.
func stampedName(name string) string {
	name, _, _ = strings.Cut(name, " - ")
	folded := mtgmatcherNormalize(name)
	if strings.HasSuffix(folded, "energy") {
		folded = strings.TrimPrefix(folded, "basic")
	}
	return folded
}

// stampedKey names a printing the way the join asks for it: the set it is
// filed in, the name, and the number with its padding off.
func stampedKey(setCode, name, number string) string {
	return setCode + "|" + stampedName(name) + "|" + unpad(number)
}

// mintFromCardmarket adds a row for every product of those shelves that no
// catalog of ours carries. It answers how many it minted and how many it
// passed over.
//
// THE FINISH ON THESE ROWS IS A GUESS, NOT A FACT. Nothing publishes it:
// Cardmarket's record has no finish field, its rarity is the constant
// "Promo" on these shelves, and CardTrader's `pokemon_reverse` says whether
// a reverse variant is sellable rather than what the card is. The decision,
// made on 2026-09-07, is that a card priced with a guessed finish beats a
// card not priced at all, to be corrected case by case as any row is found
// wrong. The guess is measured, in stampedFinish, against the stamped
// promos TCGplayer does carry.
//
// A product is minted only where the printing it was stamped from is one we
// already carry, in the set Cardmarket's number names: "PAL 097" is Paldea
// Evolved's Mimikyu and lends the promo its total, 193. Read by name and
// number alone, the source was whichever set held a Mimikyu at 097 first,
// and four SEA promos were published - and id-spelled - with another set's
// total. The set is resolved from our own codes first, then from
// pokemontcg.io's ptcgo codes by set name, since Cardmarket writes the
// ptcgo code where TCGplayer abbreviates its own way (ASR for SWSH10).
func mintFromCardmarket(path string, cards []any, sets map[string]any, ptcg []pokemontcgSet) ([]any, int, int) {
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
		code, wanted := cardmarketMintSets.Get(expansion.Name)
		if !wanted {
			continue
		}
		shelves[id] = code
	}

	// Our sets by the names the other source may write them under: the
	// whole name, and the name behind the code TCGplayer writes in front
	// ("Astral Radiance" for "SWSH10: Astral Radiance", "Forbidden Light"
	// for "SM - Forbidden Light").
	ourSetsByName := map[string][]string{}
	for code, entry := range sets {
		set, _ := entry.(map[string]any)
		name, _ := set["name"].(string)
		if name == "" {
			continue
		}
		spellings := []string{name}
		for _, sep := range []string{": ", " - "} {
			if _, rest, found := strings.Cut(name, sep); found && rest != "" {
				spellings = append(spellings, rest)
			}
		}
		for _, spelling := range spellings {
			key := mtgmatcherNormalize(spelling)
			if !slices.Contains(ourSetsByName[key], code) {
				ourSetsByName[key] = append(ourSetsByName[key], code)
			}
		}
	}
	ptcgoNames := map[string][]string{}
	for _, set := range ptcg {
		if set.PtcgoCode != "" {
			ptcgoNames[strings.ToUpper(set.PtcgoCode)] = append(ptcgoNames[strings.ToUpper(set.PtcgoCode)], set.Name)
		}
	}
	// resolve names the sets of ours a source code can mean. Several where
	// one ptcgo code covers a set and its Trainer Gallery; the printing's
	// number settles which below.
	resolve := func(code string) []string {
		if _, ours := sets[code]; ours {
			return []string{code}
		}
		var candidates []string
		for _, name := range ptcgoNames[code] {
			for _, ourCode := range ourSetsByName[mtgmatcherNormalize(name)] {
				if !slices.Contains(candidates, ourCode) {
					candidates = append(candidates, ourCode)
				}
			}
		}
		sort.Strings(candidates)
		return candidates
	}

	// What we already hold on the shelves, so a product the catalog covers
	// is passed over; what each printing was sold as, by set, so a promo
	// can take its total from the printing it was stamped from; and how
	// many products of a name a shelf holds, for the one case a number
	// cannot settle.
	held := map[string]bool{}
	heldNames := map[string]int{}
	sources := map[string][]map[string]any{}
	for _, entry := range cards {
		row, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		number, _ := row["number"].(string)
		code, _ := row["setCode"].(string)
		if !held[stampedKey(code, name, number)] {
			heldNames[code+"|"+stampedName(name)]++
		}
		held[stampedKey(code, name, number)] = true
		sources[stampedKey(code, name, number)] = append(sources[stampedKey(code, name, number)], row)
	}

	ids := make([]int, 0, len(catalog.Data.Products))
	for id := range catalog.Data.Products {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	shelfCounts := map[string]int{}
	for _, id := range ids {
		product := catalog.Data.Products[id]
		if code, wanted := shelves[product.ExpansionID]; wanted {
			shelfCounts[code+"|"+stampedName(product.Name)]++
		}
	}

	var minted, passed, heldByName, versions int
	unresolved := map[string]int{}
	mintedThisRun := map[string]bool{}
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
		sourceCode, number := strings.ToUpper(fields[1]), fields[2]
		key := stampedKey(code, name, number)
		if held[key] {
			continue
		}
		// TCGplayer files a printing under a number its own name
		// contradicts - "Professor Turo's Scenario - 171/182" numbered
		// 177 - so where the shelf holds one product of the name on each
		// side, that is the same card whatever the number says.
		if heldNames[code+"|"+stampedName(name)] == 1 && shelfCounts[code+"|"+stampedName(name)] == 1 {
			heldByName++
			continue
		}
		// A second Cardmarket product of one name and number is a further
		// version of the stamp - a later year, another pattern - which
		// the marketplace counts and this datastore has no field for.
		if mintedThisRun[key] {
			versions++
			continue
		}
		candidates := resolve(sourceCode)
		if len(candidates) == 0 {
			unresolved[sourceCode]++
			passed++
			continue
		}
		// The printing it was stamped from, which has to be one of ours,
		// in the set the number names.
		var from []map[string]any
		for _, candidate := range candidates {
			from = append(from, sources[stampedKey(candidate, name, number)]...)
		}
		if len(from) == 0 {
			passed++
			continue
		}
		// The row is the source printing stamped, so it wears that
		// printing's name and mark as this datastore spells them:
		// "Professor's Research" with the person in the mark, not
		// Cardmarket's "Professor's Research - Professor Elm".
		sourceName, _ := from[0]["name"].(string)
		if sourceName != "" {
			name = sourceName
		}
		watermark, _ := from[0]["watermark"].(string)
		total, _ := from[0]["total"].(string)
		finish := stampedFinish(code, name, from, sets)

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
			"id":      fmt.Sprintf("%s_mkm%d%s", sanitizeID(number+"-"+total), id, emit.FinishSuffix(finish)),
			"name":    name,
			"setCode": code,
			"number":  number,
			"rarity":  rarity,
			"finish":  finish,
			// The product this was minted from, and the only id it can be
			// priced by: no TCGplayer product sells a stamped promo. A
			// field, so nothing has to read it out of the uuid.
			"externalLinks": map[string]any{"cardmarketId": id},
		}
		if total != "" {
			entry["total"] = total
		}
		if watermark != "" {
			entry["watermark"] = watermark
		}
		cards = append(cards, entry)
		mintedThisRun[key] = true
		minted++
	}
	if heldByName > 0 {
		log.Printf("cardmarket: %d products are a printing TCGplayer files under a number its own name contradicts; not minted", heldByName)
	}
	if versions > 0 {
		log.Printf("cardmarket: %d products are a further version of a stamp already minted, which this datastore has no field to tell apart; not minted", versions)
	}
	if len(unresolved) > 0 {
		codes := make([]string, 0, len(unresolved))
		for code, n := range unresolved {
			codes = append(codes, fmt.Sprintf("%s (%d)", code, n))
		}
		sort.Strings(codes)
		log.Printf("cardmarket: source set codes no set of ours answers to, whose products mint nothing: %s", strings.Join(codes, ", "))
	}
	return cards, minted, passed
}

// stampedFinish guesses the finish a stamped promo is printed in, from the
// shelf it is on and the printing it was stamped from.
//
// Measured against the 63 promos of these shelves that TCGplayer carries,
// with the source read from the set Cardmarket's number names:
//
//   - A source sold in Holofoil and nothing else is a holo card - the ex
//     and Radiant Pokemon - and its stamp is holo too.
//   - The Professor Program printed plain until 2010 and reverse holo
//     since; its basic energies plain from Ruby & Sapphire and holo from
//     Scarlet & Violet. The year of the source set tells the two apart.
//   - The Southeast Asia stamps are holo on a Pokemon or a Supporter and
//     plain on an Item or a Stadium.
//
// Right on 59 of the 63; the rule it replaces - holo only where the source
// was sold in no other finish, plain otherwise - was right on 17. The four
// it misses are an Emerald-era Supporter TCGplayer sells holo, one energy
// sold plain among fifteen holo siblings, and two Professor's Research
// stamped from a promo set that is holo beside plain.
func stampedFinish(shelf, name string, from []map[string]any, sets map[string]any) string {
	finishes := map[string]bool{}
	for _, row := range from {
		finish, _ := row["finish"].(string)
		finishes[finish] = true
	}
	if len(finishes) == 1 && finishes["Holofoil"] {
		return "Holofoil"
	}
	sourceSet, _ := from[0]["setCode"].(string)
	set, _ := sets[sourceSet].(map[string]any)
	date, _ := set["releaseDate"].(string)
	modern := date >= "2011"
	if strings.HasSuffix(stampedName(name), "energy") {
		if modern {
			return "Holofoil"
		}
		return "Normal"
	}
	if shelf == "PPP" {
		if modern {
			return "Reverse Holofoil"
		}
		return "Normal"
	}
	kind, _ := from[0]["type"].(string)
	for _, word := range []string{"Item", "Stadium", "Tool"} {
		if strings.Contains(kind, word) {
			return "Normal"
		}
	}
	return "Holofoil"
}
