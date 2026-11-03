package main

import (
	"log"
	"slices"
	"sort"
	"strings"

	"github.com/mtgban/go-tcgplayer"
)

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
			if n == "" || slices.Contains(names, n) {
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

// printRunPrintings name a print run rather than a finish. TCGplayer files
// the WotC-era runs this way: a set that sold both prices every card in a
// pair of them - "1st Edition" beside "Unlimited", or the holo pair - where
// every later set prices the plain "Normal" and "Holofoil".
var printRunPrintings = map[string]bool{
	"1st Edition":          true,
	"1st Edition Holofoil": true,
	"Unlimited":            true,
	"Unlimited Holofoil":   true,
}

// dropStrayPrintRuns takes a print run off a product that sells a plain
// printing too, which is the catalog hanging a run's skus on the wrong
// product rather than a printing that exists.
//
// A run and a finish are alternatives, never both: of every product this
// category carries, one names both - Base Set's Alakazam (42346), sold as
// "Holofoil" and as "1st Edition Holofoil". The ten groups that sell both
// runs never mix them, carrying ("1st Edition", "Unlimited") 761 times, the
// holo pair 179 times and "Normal" alone 64 times; the three groups where a
// run is the exception carry it alone, which is the real thing - the two
// Machamps the starter deck sold under Deck Exclusives, a WoTC Promo
// misprint and a prerelease, none of them sold in any other printing.
//
// The run those skus name is already priced where it belongs. TCGplayer
// files the first-edition and shadowless printings as a set of their own,
// so Alakazam 001/102 is 106996 in "Base Set (Shadowless)", which prices
// "1st Edition Holofoil" itself; the entry this dropped was a second,
// unpriced copy of that printing filed under the unlimited set, and a
// lookup naming Base Set and the run answered with it instead.
func dropStrayPrintRuns(c *tcgplayer.CatalogDump, printings map[int][]string) int {
	name := map[int]string{}
	for _, p := range c.Products {
		name[p.ProductID] = p.Name
	}
	// Sorted, so the log a build is compared by reads the same every time.
	ids := make([]int, 0, len(printings))
	for id := range printings {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	var dropped int
	for _, id := range ids {
		var kept, left []string
		for _, n := range printings[id] {
			if printRunPrintings[n] {
				left = append(left, n)
				continue
			}
			kept = append(kept, n)
		}
		if len(left) == 0 || len(kept) == 0 {
			continue
		}
		printings[id] = kept
		dropped += len(left)
		log.Printf("print run: %q (%d) names %s beside %s; the run is the set it is filed under, not a printing here",
			name[id], id, strings.Join(left, ", "), strings.Join(kept, ", "))
	}
	return dropped
}

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
