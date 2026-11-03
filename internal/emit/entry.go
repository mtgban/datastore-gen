package emit

import (
	"fmt"
	"log"
	"strings"
)

// Datastore is the document six of the eight games publish under the
// envelope's data (SPECIFICATIONS §2): every game but Riftbound and Lorcana,
// which publish their upstream payload whole. Its fields are encoded in the
// order they are declared, which is the order §2 lists them in, rather than
// sorted as a map's keys are.
type Datastore struct {
	Game       string              `json:"game"`
	Properties map[string][]string `json:"properties"`
	Sets       map[string]*Set     `json:"sets"`
	Cards      []*Card             `json:"cards"`
	Sealed     []*Sealed           `json:"sealed"`
}

// Set is one entry of sets, keyed by its set code (SPECIFICATIONS §2.1). A
// set no source dates publishes no releaseDate.
type Set struct {
	Name         string `json:"name"`
	ReleaseDate  string `json:"releaseDate,omitempty"`
	Type         string `json:"type,omitempty"`
	BaseSetSize  int    `json:"baseSetSize,omitempty"`
	Abbreviation string `json:"abbreviation,omitempty"`
	Symbol       string `json:"symbol,omitempty"`
}

// Card is one card entry (SPECIFICATIONS §2.2): the fields every game
// shares, then the game facts each publishes under its own name. A field
// is published only where it is set, but for the five every entry carries.
type Card struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	SetCode             string   `json:"setCode"`
	Number              string   `json:"number,omitempty"`
	Total               string   `json:"total,omitempty"`
	Rarity              string   `json:"rarity"`
	Finish              string   `json:"finish"`
	Variant             string   `json:"variant,omitempty"`
	PromoTypes          []string `json:"promoTypes,omitempty"`
	Watermark           string   `json:"watermark,omitempty"`
	Language            string   `json:"language,omitempty"`
	OriginalReleaseDate string   `json:"originalReleaseDate,omitempty"`

	Type       string   `json:"type,omitempty"`
	Types      []string `json:"types,omitempty"`
	Color      string   `json:"color,omitempty"`
	Colors     []string `json:"colors,omitempty"`
	Pitch      string   `json:"pitch,omitempty"`
	Pitches    []string `json:"pitches,omitempty"`
	Attribute  string   `json:"attribute,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Artist     string   `json:"artist,omitempty"`

	Image         string `json:"image,omitempty"`
	ExternalLinks Links  `json:"externalLinks,omitzero"`
}

// Sealed is one sealed product (SPECIFICATIONS §2.3), every field always
// published.
type Sealed struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SetCode       string `json:"setCode"`
	ReleaseDate   string `json:"releaseDate"`
	Image         string `json:"image"`
	ExternalLinks Links  `json:"externalLinks"`
}

// Printing is one finish of a card in the two games that publish their
// upstream's cards whole, Lorcana and Riftbound (SPECIFICATIONS §2.5): the
// finish TCGplayer prices it under, the uuid it is quoted by, and the
// treatments it is the printing of.
type Printing struct {
	Finish     string   `json:"finish"`
	ID         string   `json:"id"`
	PromoTypes []string `json:"promoTypes,omitempty"`
}

// Links is an entry's externalLinks: the TCGplayer product on every priced
// entry, and the upstream's id beside it or in its place. A minted entry
// links nothing and publishes no externalLinks at all.
type Links struct {
	TCGPlayerID  int    `json:"tcgPlayerId,omitempty"`
	FabID        string `json:"fabId,omitempty"`
	TcgdexID     string `json:"tcgdexId,omitempty"`
	CardmarketID int    `json:"cardmarketId,omitempty"`
	BandaiID     string `json:"bandaiId,omitempty"`
	KonamiID     int    `json:"konamiId,omitempty"`
	CardTraderID int    `json:"cardTraderId,omitempty"`
}

// Lists gives every card carrying a colour, a pitch or an attribute the
// same values as the list a reader takes, split where the string joins two
// with a semicolon ("Green;Red"). A card already holding the list keeps it.
func (d *Datastore) Lists() {
	for _, c := range d.Cards {
		for _, field := range []struct {
			value string
			list  *[]string
		}{{c.Color, &c.Colors}, {c.Pitch, &c.Pitches}, {c.Attribute, &c.Attributes}} {
			if field.value != "" && len(*field.list) == 0 {
				*field.list = strings.Split(field.value, ";")
			}
		}
	}
}

// SpellRarities publishes every card's rarity by its name where it holds a
// code names spells out. A builder runs it last, so whatever compared the
// codes on the way still read them.
func (d *Datastore) SpellRarities(names map[string]string) {
	for _, c := range d.Cards {
		if name, found := names[c.Rarity]; found {
			c.Rarity = name
		}
	}
}

// DropRepeatedFinishes keeps the first card pricing each product's finish
// and logs the rest. A product sells a finish once, so a second entry for it
// is a printing nothing tells apart from the first and splits its price; it
// is dropped rather than refusing every card of the game over one product.
// A card no product prices is kept as it is.
func (d *Datastore) DropRepeatedFinishes() {
	type printing struct {
		product int
		finish  string
	}
	seen := map[printing]bool{}
	kept := make([]*Card, 0, len(d.Cards))
	var dropped []string
	for _, c := range d.Cards {
		key := printing{c.ExternalLinks.TCGPlayerID, c.Finish}
		if key.product != 0 && key.finish != "" && seen[key] {
			dropped = append(dropped, fmt.Sprintf("%v (product %d %s)", c.ID, key.product, key.finish))
			continue
		}
		seen[key] = true
		kept = append(kept, c)
	}
	if len(dropped) > 0 {
		log.Printf("finishes: %d entries price a product's finish another entry already prices, dropped: %s",
			len(dropped), strings.Join(dropped, ", "))
	}
	d.Cards = kept
}

// DropEmptySets deletes every set no card and no sealed product names as
// its setCode, and logs them: a set holding neither is one nothing can be
// found in.
func (d *Datastore) DropEmptySets() {
	inUse := map[string]bool{}
	for _, c := range d.Cards {
		inUse[c.SetCode] = true
	}
	for _, product := range d.Sealed {
		inUse[product.SetCode] = true
	}
	dropEmptySets(d.Sets, inUse)
}
