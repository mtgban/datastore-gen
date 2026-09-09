package vocabulary

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrNotDatastore says a file is not a built datastore. The games whose
// upstream is itself a JSON document keep that document under the same name,
// and reading one as a datastore reports an empty vocabulary rather than
// saying it read the wrong thing.
var ErrNotDatastore = errors.New("no cards: this is not a built datastore")

// aside are the fields that are no part of what makes two printings alike.
//
// Some name a printing rather than describe it - its id, the pictures of it,
// the identifiers it carries upstream. Every one of them tells any two
// printings apart and a listing names none of them, so counting them would
// report every datastore clean.
//
// And one is prose: the variant is the label the catalog wrote, which is
// what the promo types, the mark and the date are distilled out of. A
// printing told from its siblings by nothing but that sentence is the thing
// this check is looking for, so the sentence cannot be what tells them
// apart.
var aside = map[string]bool{
	"id": true, "image": true, "images": true, "externalLinks": true,
	"printings": true, "fabId": true, "bandaiId": true, "code": true,
	"fullIdentifier": true, "variant": true,
}

// cardsOf finds a datastore's cards, in either place a game keeps them.
//
// Most write them at the top. Riftbound's upstream is the card gallery Riot
// serves its own site, and the builder publishes that document with the
// cards where they already were - so a reader that stops at the top level
// sees none and reports the whole game clean.
func cardsOf(payload map[string]any) []map[string]any {
	if held, found := payload["cards"]; found {
		return objects(held)
	}
	page, _ := payload["pageProps"].(map[string]any)
	held, _ := page["page"].(map[string]any)
	for _, blade := range objects(held["blades"]) {
		gallery, _ := blade["cards"].(map[string]any)
		if items := objects(gallery["items"]); len(items) > 0 {
			return items
		}
	}
	return nil
}

// objects reads a field as the list of cards it holds.
func objects(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		if card, ok := one.(map[string]any); ok {
			out = append(out, card)
		}
	}
	return out
}

// SetNames reads the names a datastore gives its sets, and the name behind
// a code where it writes one in front: a label says "Twilight Masquerade"
// for a set this datastore calls "SV06: Twilight Masquerade".
func SetNames(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Sets map[string]struct {
			Name string `json:"name"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var names []string
	for _, set := range payload.Sets {
		if set.Name == "" {
			continue
		}
		names = append(names, set.Name)
		if _, rest, found := strings.Cut(set.Name, ": "); found && rest != "" {
			names = append(names, rest)
		}
	}
	return names, nil
}

// ReadDatastore reads a published datastore as the printings it holds.
func ReadDatastore(path string) ([]Printing, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	cards := cardsOf(payload)
	if len(cards) == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNotDatastore)
	}
	var out []Printing
	for _, card := range cards {
		facts := map[string]any{}
		for key, value := range card {
			if aside[key] || strings.HasSuffix(key, "Id") || strings.HasSuffix(key, "ID") {
				continue
			}
			facts[key] = fmt.Sprint(value)
		}
		printing := Printing{
			ID:         say(card["id"]),
			Rarity:     say(card["rarity"]),
			Finish:     say(card["finish"]),
			PromoTypes: tokensOf(card["promoTypes"]),
			Facts:      facts,
		}
		sold, several := card["printings"].([]any)
		if !several || len(sold) == 0 {
			out = append(out, printing)
			continue
		}
		// A card saying its finishes in a printings array is several
		// printings and is read as several: read as one, every finish of it
		// carries the same empty finish and all of them look alike.
		for _, one := range sold {
			held, ok := one.(map[string]any)
			if !ok {
				continue
			}
			finished := printing
			finished.ID = say(held["id"])
			finished.Finish = say(held["finish"])
			finished.Facts = map[string]any{}
			for key, value := range facts {
				finished.Facts[key] = value
			}
			finished.Facts["finish"] = finished.Finish
			out = append(out, finished)
		}
	}
	return out, nil
}

// tokensOf reads a card's promo types, which a datastore holds as a list of
// strings and a decoded document as a list of anything.
func tokensOf(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, token := range list {
		out = append(out, say(token))
	}
	return out
}

// say writes a field as a string, whichever way the game wrote it.
func say(field any) string {
	if field == nil {
		return ""
	}
	if number, ok := field.(float64); ok && number == float64(int64(number)) {
		return fmt.Sprintf("%d", int64(number))
	}
	return fmt.Sprint(field)
}
