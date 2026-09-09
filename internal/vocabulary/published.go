package vocabulary

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ErrNotDatastore says a file is not a built datastore. The games whose
// upstream is itself a JSON document keep that document under the same name,
// and reading one as a datastore reports an empty vocabulary rather than
// saying it read the wrong thing.
var ErrNotDatastore = errors.New("no cards: this is not a built datastore")

// published is the part of a card these checks read. The games disagree on
// the type of an id, a number and a set code - some write them as numbers -
// so each is taken as it comes and said as a string.
type published struct {
	ID                  any      `json:"id"`
	Name                string   `json:"name"`
	Number              any      `json:"number"`
	SetCode             any      `json:"setCode"`
	Rarity              string   `json:"rarity"`
	Finish              string   `json:"finish"`
	PromoTypes          []string `json:"promoTypes"`
	Watermark           string   `json:"watermark"`
	OriginalReleaseDate string   `json:"originalReleaseDate"`
	Language            string   `json:"language"`

	// Printings is the other shape a datastore says a finish in: one entry
	// per priced finish, beside the id that prices it. A card carrying it
	// is several printings and has to be read as several, or every one of
	// them reads as the same finish and they all look alike.
	Printings []struct {
		Finish string `json:"finish"`
		ID     string `json:"id"`
	} `json:"printings"`
}

// ReadDatastore reads a published datastore as the printings it holds.
func ReadDatastore(path string) ([]Printing, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Cards []published `json:"cards"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if len(payload.Cards) == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNotDatastore)
	}
	var out []Printing
	for _, card := range payload.Cards {
		printing := Printing{
			ID:         say(card.ID),
			Name:       card.Name,
			Number:     say(card.Number),
			SetCode:    say(card.SetCode),
			Rarity:     card.Rarity,
			Finish:     card.Finish,
			PromoTypes: card.PromoTypes,
			Watermark:  card.Watermark,
			Date:       card.OriginalReleaseDate,
			Language:   card.Language,
		}
		if len(card.Printings) == 0 {
			out = append(out, printing)
			continue
		}
		for _, sold := range card.Printings {
			finished := printing
			finished.Finish, finished.ID = sold.Finish, sold.ID
			out = append(out, finished)
		}
	}
	return out, nil
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
