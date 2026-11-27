package main

import (
	"fmt"
	"log"
	"maps"
	"slices"

	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/handtable"
	"github.com/mtgban/go-tcgplayer"
)

// mergedProducts is a product TCGplayer deleted as a duplicate of another,
// keyed by the deleted id, with the product that sells the card now.
// pokemonpricetracker still files eBay sales under the deleted id, so every
// entry of the survivor carries it in formerTcgPlayerIds. Each pair is the
// deleted product's name, number and image matched against the catalog's.
var mergedProducts = handtable.New("mergedProducts", map[int]int{
	664093: 666538, // Charcadet 022, the same image
	611828: 653353, // Jolteon 169
	189757: 609698, // Eevee SM184
	662234: 660409, // Toxtricity 068/094
	662148: 660414, // Dawn 087/094
	577571: 656432, // Raging Bolt ex 123/162 (Wrong Image)
	672359: 44418,  // Aerodactyl 01/62
	646307: 642351, // Klinklang 063/086 (Master Ball Pattern)
	646306: 642423, // Klinklang 063/086 (Poke Ball Pattern)
	535897: 512914, // Houndstone 042
	662168: 660408, // Haunter 055/094
	660410: 660495, // Gastly 054/094, filed as "Ghastly"
	600428: 635612, // Ponyta 014/083 with the Celebrations stamp
	649189: 619070, // Colress's Tenacity 057/064
	490484: 515539, // Boss's Orders [Cyrus] 132/172
	607890: 594471, // Tapu Koko 065/191
	623341: 220092, // Tapu Koko 061/189
	607889: 594470, // Gouging Fire 038/191
	664013: 661700, // Spiritomb 087/132, the Trick or Trade stamp
	498051: 86835,  // Lotad 55/100, its reverse holofoil
	85378:  126301, // Fairy Energy, unnumbered
	260710: 247469, // Mimikyu 081/189
})

// deletedProduct is a card as the deleted product sold it: the group it
// was filed in, and the fields its entry publishes.
type deletedProduct struct {
	group                                           int
	name, number, rarity, finish, cardType, variant string
	promoTypes                                      []string
}

// deletedJapanese is a Japanese-exclusive product TCGplayer deleted from
// this category, keyed by the deleted id; its Pokemon Japan category sells
// each under an id of its own. Nothing here sells the card any more, so it
// is minted, in Japanese, carrying the deleted id in formerTcgPlayerIds and
// the id its priced entry was spelled with.
var deletedJapanese = handtable.New("deletedJapanese", map[int]deletedProduct{
	489919: {1861, "Red's Pikachu", "270/SM-P", "Promo", "Holofoil", "Lightning", "", nil},
	494025: {1861, "Shaymin", "225/SM-P", "Promo", "Holofoil", "Grass", "", nil},
	250510: {2374, "Pikachu", "208/S-P", "Promo", "Reverse Holofoil", "Lightning", "Yu Nagaba", nil},
	480659: {2374, "Gardevoir", "408/SM-P", "Promo", "Normal", "Fairy", "JP Illustration Grand Prix Winner 2019", nil},

	// The Ash vs Team Rocket Deck Kit, whose group the catalog keeps empty.
	491181: {23095, "Litten", "059/SM-P", "Promo", "Normal", "Fire", "", nil},
	490239: {23095, "Rowlet", "001/026", "Common", "Normal", "Grass", "", nil},
	490259: {23095, "Mareanie", "002/026", "Common", "Normal", "Water", "", nil},
	490260: {23095, "Toxapex", "003/026", "Common", "Normal", "Water", "", nil},
	490263: {23095, "Bruxish", "004/026", "Common", "Normal", "Water", "", nil},
	490242: {23095, "Ash's Pikachu GX", "005/026", "Common", "Holofoil", "Lightning", "", nil},
	490243: {23095, "Togedemaru", "006/026", "Common", "Normal", "Lightning", "", nil},
	490244: {23095, "Tapu Koko", "007/026", "Common", "Normal", "Lightning", "", nil},
	490264: {23095, "Wobbuffet", "008/026", "Common", "Normal", "Psychic", "", nil},
	490265: {23095, "Salandit", "009/026", "Common", "Normal", "Psychic", "", nil},
	490267: {23095, "Team Rocket's Mimikyu GX", "010/026", "Common", "Holofoil", "Psychic", "", nil},
	490245: {23095, "Rockruff", "011/026", "Common", "Normal", "Fighting", "", nil},
	490246: {23095, "Lycanroc", "012/026", "Common", "Normal", "Fighting", "", nil},
	490271: {23095, "Meowth", "013/026", "Common", "Normal", "Colorless", "", nil},
	490248: {23095, "Tauros", "014/026", "Common", "Normal", "Colorless", "", nil},
	490249: {23095, "Yungoos", "015/026", "Common", "Normal", "Colorless", "", nil},
	490250: {23095, "Gumshoos", "016/026", "Common", "Normal", "Colorless", "", nil},
	490276: {23095, "Stufful", "017/026", "Common", "Normal", "Colorless", "", nil},
	490277: {23095, "Bewear", "018/026", "Common", "Normal", "Colorless", "", nil},
	490255: {23095, "Ash's Friendship", "025/026", "Common", "Normal", "Supporter", "", nil},
	490280: {23095, "Team Rocket's Harassment", "026/026", "Common", "Normal", "Supporter", "", nil},

	// Left the category on 2026-11-26, published the day before as these rows.
	252737: {1451, "Poncho-wearing Pikachu", "208/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	252738: {1451, "Poncho-wearing Pikachu", "207/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	268123: {1451, "Poncho-wearing Pikachu", "203/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	268124: {1451, "Poncho-wearing Pikachu", "230/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	268126: {1451, "Poncho-wearing Pikachu", "231/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	268127: {1451, "Poncho-wearing Pikachu", "274/XY-P", "Promo", "Normal", "Lightning", "", nil},
	268258: {1451, "Poncho-wearing Pikachu", "275/XY-P", "Promo", "Normal", "Lightning", "", nil},
	268259: {1451, "Pretend Gyarados Pikachu", "151/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	268260: {1451, "Pretend Magikarp Pikachu", "150/XY-P", "Promo", "Holofoil", "Lightning", "", nil},
	268261: {1451, "Mega Tokyo's Pikachu", "98/XY-P", "Promo", "Normal", "Lightning", "", nil},
	268262: {1451, "Mega Tokyo's Pikachu", "204/XY-P", "Promo", "Normal", "Lightning", "", nil},
	489917: {1451, "Hoopa", "155/XY-P", "Promo", "Holofoil", "Psychic", "", nil},
	479881: {1528, "Latias", "195/172", "Promo", "Holofoil", "Dragon", "Vstar Universe Japanese Exclusive", []string{"vstaruniversejapaneseexclusive"}},
	268129: {1861, "Poncho-wearing Pikachu", "37/SM-P", "Promo", "Holofoil", "Lightning", "", nil},
	268131: {1861, "Poncho-wearing Pikachu", "38/SM-P", "Promo", "Holofoil", "Lightning", "", nil},
	268303: {1861, "Tohoku's Pikachu", "88/SM-P", "Promo", "Holofoil", "Lightning", "", nil},
	276363: {1861, "Clefairy", "381/SM-P", "Promo", "Holofoil", "Fairy", "Dream League", []string{"dreamleague"}},
	478250: {2374, "Charizard", "054/ADV-P", "Promo", "Normal", "Fire", "Japanese Meiji Chocolate Exclusive Promo", []string{"meijichocolateexclusivepromo"}},
	484495: {2374, "Alolan Ninetales", "145/236", "Holo Rare", "Holofoil", "Fairy", "SM-P Japanese Exclusive", []string{"smpjapaneseexclusive"}},
	484828: {2374, "Icy Sky's Shaymin", "001/009", "Promo", "Holofoil", "Grass", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484829: {2374, "Piplup", "002/009", "Promo", "Holofoil", "Water", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484830: {2374, "Pikachu", "003/009", "Promo", "Holofoil", "Lightning", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484944: {2374, "Magnezone", "004/009", "Promo", "Holofoil", "Lightning", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484945: {2374, "Reverse World's Giratina", "005/009", "Promo", "Holofoil", "Psychic", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484950: {2374, "Mamoswine", "006/009", "Promo", "Holofoil", "Fighting", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484953: {2374, "Shieldon", "007/009", "Promo", "Holofoil", "Metal", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	484954: {2374, "Dialga", "008/009", "Promo", "Holofoil", "Metal", "Japanese 11th Movie Commemoration Set", []string{"11thmoviecommemorationset"}},
	487024: {2374, "Gardevoir ex", "101/078 SAR", "Promo", "Holofoil", "Psychic", "Japanese Exclusive", nil},
	489945: {2374, "Marnie", "340/S-P", "Promo", "Normal", "Supporter", "", nil},
	489946: {2374, "Mallow & Lana", "256/S-P", "Promo", "Normal", "Supporter", "", nil},
	489947: {2374, "Cynthia", "155/S-P", "Promo", "Normal", "Supporter", "", nil},
})

// liveProducts is every product id the catalog lists. A deleted id found
// here is a product again, and its rows are left to the catalog.
func liveProducts(catalog *tcgplayer.CatalogDump) map[int]bool {
	live := map[int]bool{}
	for _, product := range catalog.Products {
		live[product.ProductID] = true
	}
	return live
}

// mintDeleted adds an entry for each deleted Japanese product, in the set of
// the group it was filed in, publishing that set from the group where the
// group is empty now.
func mintDeleted(groups []tcgplayer.Group, setCodes map[int]string, live map[int]bool,
	sets map[string]*emit.Set, cards []*emit.Card, cardTypes *printedTypes) []*emit.Card {
	byID := map[int]tcgplayer.Group{}
	for _, group := range groups {
		byID[group.GroupID] = group
	}
	var minted, setsMinted int
	for _, id := range slices.Sorted(maps.Keys(deletedJapanese.Rows())) {
		row := deletedJapanese.Rows()[id]
		if live[id] {
			log.Printf("deleted: %d (%s) is in the catalog again and is not minted", id, row.name)
			continue
		}
		group, listed := byID[row.group]
		if !listed {
			log.Printf("deleted: %d (%s) names group %d, which the catalog no longer lists; not minted", id, row.name, row.group)
			continue
		}
		deletedJapanese.Use(id)
		code := setCodes[row.group]
		if sets[code] == nil {
			set := &emit.Set{Name: group.Name, Abbreviation: group.Abbreviation}
			if emit.HasDate(group) {
				set.ReleaseDate = group.ReleaseDate()
			}
			sets[code] = set
			setsMinted++
		}
		entry := &emit.Card{
			ID:            emit.IDBase(row.number, id) + emit.FinishSuffix(row.finish),
			Name:          row.name,
			SetCode:       code,
			Rarity:        row.rarity,
			Finish:        row.finish,
			Variant:       row.variant,
			PromoTypes:    row.promoTypes,
			Language:      "Japanese",
			Type:          row.cardType,
			Types:         cardTypes.of(nil, row.cardType),
			Image:         emit.ImageURL(fmt.Sprintf("https://tcgplayer-cdn.tcgplayer.com/product/%d_200w.jpg", id)),
			ExternalLinks: emit.Links{FormerTCGPlayerIDs: []int{id}},
		}
		emitNumber(entry, emit.QueryNumber(row.number))
		cards = append(cards, entry)
		minted++
	}
	log.Printf("deleted: %d Japanese products TCGplayer deleted from this category minted, %d sets published for their emptied groups",
		minted, setsMinted)
	return cards
}

// linkMerged gives every entry of a product a deleted duplicate was merged
// into the deleted id. A row whose survivor the build does not carry is
// left unused, and reported as such.
func linkMerged(live map[int]bool, cards []*emit.Card) {
	byProduct := map[int][]*emit.Card{}
	for _, entry := range cards {
		if id := entry.ExternalLinks.TCGPlayerID; id != 0 {
			byProduct[id] = append(byProduct[id], entry)
		}
	}
	var linked int
	for _, old := range slices.Sorted(maps.Keys(mergedProducts.Rows())) {
		survivor := mergedProducts.Rows()[old]
		if live[old] {
			log.Printf("deleted: %d is in the catalog again and is not linked to %d", old, survivor)
			continue
		}
		entries := byProduct[survivor]
		if len(entries) == 0 {
			continue
		}
		mergedProducts.Use(old)
		for _, entry := range entries {
			entry.ExternalLinks.FormerTCGPlayerIDs = append(entry.ExternalLinks.FormerTCGPlayerIDs, old)
		}
		linked++
	}
	log.Printf("deleted: %d deleted duplicates linked to the product selling their card", linked)
}
