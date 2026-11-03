package main

import "github.com/mtgban/datastore-gen/internal/handtable"

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
//
// A row is used only where it changes the name, so one the catalog has
// come to spell itself is reported with the ones whose product is gone.
func handName(productID int, name string) string {
	if corrected, hand := handNames.Rows()[productID]; hand && corrected != name {
		handNames.Use(productID)
		return corrected
	}
	return name
}

var handNames = handtable.New("handNames", map[int]string{
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
})

// handDates are release dates for the groups neither source can date: the
// catalog stamps its request time on them and tcgdex has no set to join.
// Each is researched, not guessed, and keyed by the group id, which the
// catalog never reuses. A group holding one promotion carries that
// promotion's release day; a drawer TCGplayer fills across eras carries
// the earliest day its contents can have existed, so it sorts where its
// oldest cards belong. A row is used only where no source dates its group.
var handDates = handtable.New("handDates", map[int]string{
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
})
