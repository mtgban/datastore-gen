// Package validate re-reads a builder's encoded output and refuses one a
// consumer would misread: what every builder checks, with the game's own
// rules handed in. A document whose cards sit at its top, one entry per
// printing, is read by Flat; a game whose document holds them otherwise
// reads its own into a Document, one Card per printing, and hands it to
// Check.
//
// The checks every game shares are here once - a set without a name or with
// a code a query cannot carry, a card without an id, a name or a finish, an
// id or a number a query cannot carry, an id used twice, products wearing
// one identity, a card in a set the file lacks, a product's finish emitted
// twice or not exactly as its skus sell it, the zero-skip invariant, and the
// same of the sealed products. What a game adds - which fields make a
// card's identity, how a minted card is told apart, a check of its own - is
// its Rules.
package validate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// codeShape is a set code a query can carry.
var codeShape = regexp.MustCompile(`^[A-Z0-9-]+$`)

// idShape is a uuid a query can carry.
var idShape = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// SetCode reports whether a set code is one a query can carry.
func SetCode(code string) bool {
	return codeShape.MatchString(code)
}

// Card is a card entry as the checks read it: the fields every game writes,
// and the whole entry for the ones a game's rules read.
type Card struct {
	ID, Name, Number, SetCode, Finish string
	TcgPlayerID                       int
	Entry                             map[string]any
}

// Field reads one of the entry's fields as a string, "" where it is absent.
func (c Card) Field(name string) string {
	return say(c.Entry[name])
}

// Link reads one of the entry's externalLinks as a string, "" where it is
// absent.
func (c Card) Link(name string) string {
	links, _ := c.Entry["externalLinks"].(map[string]any)
	return say(links[name])
}

// Sealed is a sealed entry as the checks read it.
type Sealed struct {
	ID, Name, SetCode string
	TcgPlayerID       int
}

// Rules is what a game adds to the shared checks.
type Rules struct {
	// Game is the document's own game field.
	Game string
	// Identity is the fields, in order, a query resolves a card by. Two
	// products alike in all of them are one card to every consumer.
	Identity []string
	// Minted tells a printing no product sells from the others alike in
	// identity; nil keys each on its own id.
	Minted func(Card) string
	// Prepare reads every card before any is checked, for a rule that needs
	// the whole file.
	Prepare func([]Card)
	// Check is the game's own check of one card, after the presence of its
	// id, name and finish.
	Check func(Card) error
	// Finally runs once every card and sealed product has passed.
	Finally func([]Card, []Sealed, map[string]string) error
}

// Counts is what a validated datastore holds.
type Counts struct {
	Sets, Cards, Sealed int
}

// Document is a datastore as the checks read it: its game, its sets' names
// by code, one Card per printing, and its sealed products.
type Document struct {
	Game   string
	Sets   map[string]string
	Cards  []Card
	Sealed []Sealed
}

// Datastore checks an encoded datastore whose cards sit at the top of the
// document against the shared rules and the game's, and against the
// finishes each catalog card product sells.
func Datastore(data []byte, wantFinishes map[int][]string, rules Rules) (Counts, error) {
	doc, err := Flat(data)
	if err != nil {
		return Counts{}, err
	}
	return Check(doc, wantFinishes, rules)
}

// Flat reads an encoded datastore whose cards sit at the top of the
// document, one entry per printing.
func Flat(data []byte) (Document, error) {
	var out Document
	data, err := emit.Unwrap(data)
	if err != nil {
		return out, err
	}
	var doc struct {
		Game string `json:"game"`
		Sets map[string]struct {
			Name string `json:"name"`
		} `json:"sets"`
		Cards  []map[string]any `json:"cards"`
		Sealed []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SetCode       string `json:"setCode"`
			ExternalLinks struct {
				TcgPlayerID int `json:"tcgPlayerId"`
			} `json:"externalLinks"`
		} `json:"sealed"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	out.Game = doc.Game
	out.Sets = map[string]string{}
	for code, set := range doc.Sets {
		out.Sets[code] = set.Name
	}
	out.Cards = make([]Card, len(doc.Cards))
	for i, entry := range doc.Cards {
		out.Cards[i] = Card{
			ID: say(entry["id"]), Name: say(entry["name"]), Number: say(entry["number"]),
			SetCode: say(entry["setCode"]), Finish: say(entry["finish"]), Entry: entry,
		}
		if links, ok := entry["externalLinks"].(map[string]any); ok {
			if id, ok := links["tcgPlayerId"].(float64); ok {
				out.Cards[i].TcgPlayerID = int(id)
			}
		}
	}
	out.Sealed = make([]Sealed, len(doc.Sealed))
	for i, product := range doc.Sealed {
		out.Sealed[i] = Sealed{ID: product.ID, Name: product.Name, SetCode: product.SetCode, TcgPlayerID: product.ExternalLinks.TcgPlayerID}
	}
	return out, nil
}

// Check checks a document against the shared rules and the game's, and
// against the finishes each catalog card product sells.
func Check(doc Document, wantFinishes map[int][]string, rules Rules) (Counts, error) {
	var out Counts
	if doc.Game != rules.Game {
		return out, fmt.Errorf("game is %q, not %s", doc.Game, rules.Game)
	}
	setNames := doc.Sets
	for code, name := range setNames {
		if name == "" {
			return out, fmt.Errorf("set %s missing its name", code)
		}
		if !codeShape.MatchString(code) {
			return out, fmt.Errorf("set code %q holds what a query cannot carry", code)
		}
	}
	cards := doc.Cards
	if rules.Prepare != nil {
		rules.Prepare(cards)
	}

	cardIDs := map[string]bool{}
	// A query resolves a card by its identity fields, never by the id, so
	// two products wearing all of them alike are one card to every consumer
	// and would alias each other's prices. The key holds the product rather
	// than a flag, so a product's own finishes pass while two different
	// products never do.
	identities := map[string]string{}
	var shared emit.SharedIdentities
	gotFinishes := map[int][]string{}
	for _, card := range cards {
		// The number is not required: some games hand out cards they give
		// no collector number, and those are carried on the id their
		// product alone mints.
		if card.ID == "" || card.Name == "" || card.Finish == "" {
			return out, fmt.Errorf("card %q (%s) missing identity", card.Name, card.ID)
		}
		if rules.Check != nil {
			if err := rules.Check(card); err != nil {
				return out, err
			}
		}
		if !idShape.MatchString(card.ID) {
			return out, fmt.Errorf("card %q has a uuid nothing can carry: %q", card.Name, card.ID)
		}
		if strings.ContainsAny(card.Number, " \t") {
			return out, fmt.Errorf("card %q (%s) has a collector number a query cannot carry: %q",
				card.Name, card.ID, card.Number)
		}
		if cardIDs[card.ID] {
			return out, fmt.Errorf("duplicate card id %s", card.ID)
		}
		cardIDs[card.ID] = true
		fields := make([]string, len(rules.Identity))
		for i, field := range rules.Identity {
			fields[i] = card.Field(field)
		}
		identity := strings.Join(fields, "|")
		// A minted printing sells as no product, so keying it on the absent
		// product id would make every one of them the same card and wave
		// through exactly the collision this catches.
		bearer := fmt.Sprintf("product %d", card.TcgPlayerID)
		if card.TcgPlayerID == 0 {
			bearer = "card " + card.ID
			if rules.Minted != nil {
				bearer = rules.Minted(card)
			}
		}
		if other, seen := identities[identity]; seen && other != bearer {
			shared.Add(other, bearer, identity)
		} else {
			identities[identity] = bearer
		}
		if _, found := setNames[card.SetCode]; !found {
			return out, fmt.Errorf("card %q in unknown set %s", card.Name, card.SetCode)
		}
		// Only products are counted against the catalog's skus: a minted
		// printing answers to no product.
		if card.TcgPlayerID == 0 {
			continue
		}
		if slices.Contains(gotFinishes[card.TcgPlayerID], card.Finish) {
			return out, fmt.Errorf("product %d carries finish %q twice", card.TcgPlayerID, card.Finish)
		}
		gotFinishes[card.TcgPlayerID] = append(gotFinishes[card.TcgPlayerID], card.Finish)
	}
	if err := shared.Check(); err != nil {
		return out, err
	}
	if err := emit.Coverage(gotFinishes, wantFinishes); err != nil {
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

	sealed := doc.Sealed
	sealedIDs := map[string]bool{}
	for _, product := range sealed {
		if product.ID == "" || product.Name == "" || product.TcgPlayerID == 0 {
			return out, fmt.Errorf("sealed %q (%s) missing identity", product.Name, product.ID)
		}
		if !idShape.MatchString(product.ID) {
			return out, fmt.Errorf("sealed %q has a uuid nothing can carry: %q", product.Name, product.ID)
		}
		if sealedIDs[product.ID] {
			return out, fmt.Errorf("duplicate sealed id %s", product.ID)
		}
		sealedIDs[product.ID] = true
		if _, found := setNames[product.SetCode]; !found {
			return out, fmt.Errorf("sealed %q in unknown set %s", product.Name, product.SetCode)
		}
	}
	if rules.Finally != nil {
		if err := rules.Finally(cards, sealed, setNames); err != nil {
			return out, err
		}
	}
	out.Sets = len(doc.Sets)
	out.Cards = len(doc.Cards)
	out.Sealed = len(doc.Sealed)
	return out, nil
}

// say writes a field as a string, whichever JSON type it was.
func say(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprint(int64(v))
		}
		return fmt.Sprint(v)
	}
	return fmt.Sprint(value)
}
