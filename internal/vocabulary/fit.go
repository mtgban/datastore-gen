package vocabulary

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// Fit sets aside the tokens Check would refuse, one printing at a time, so a
// single product's label cannot stop a game's publish: a token that is no
// slug, a long one holding a set's name, or one saying its printing's rarity
// or finish comes off that printing, and the catalog's wording stays in its
// variant. A builder runs it on its encoded output; it hands the bytes back
// untouched when nothing has to come off, and otherwise the document
// re-encoded with a line for each token it set aside.
func Fit(encoded []byte) ([]byte, []string, error) {
	var whole struct {
		Meta struct {
			Date string `json:"date"`
		} `json:"meta"`
		Data map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&whole); err != nil {
		return nil, nil, err
	}
	published := publishedSlugs(setNamesOf(whole.Data))

	var setAside []string
	for _, card := range cardsOf(whole.Data) {
		rarity := Slug(say(card["rarity"]))
		sold := objects(card["printings"])
		finishes := []string{Slug(say(card["finish"]))}
		for _, printing := range sold {
			finishes = append(finishes, Slug(say(printing["finish"])))
		}
		setAside = append(setAside, fit(card, rarity, finishes, published)...)
		for _, printing := range sold {
			setAside = append(setAside, fit(printing, rarity, []string{Slug(say(printing["finish"]))}, published)...)
		}
	}
	if len(setAside) == 0 {
		return encoded, nil, nil
	}
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(emit.Envelope(whole.Meta.Date, whole.Data)); err != nil {
		return nil, nil, err
	}
	return out.Bytes(), setAside, nil
}

// fit keeps the tokens of one card or printing that Check reads as tokens,
// and says what it set aside and why.
func fit(object map[string]any, rarity string, finishes, published []string) []string {
	held, ok := object["promoTypes"].([]any)
	if !ok {
		return nil
	}
	kept := make([]any, 0, len(held))
	var setAside []string
	for _, value := range held {
		token := say(value)
		if why := refusal(token, rarity, finishes, published); why != "" {
			setAside = append(setAside, fmt.Sprintf("%q set aside from %s: %s", token, say(object["id"]), why))
			continue
		}
		kept = append(kept, value)
	}
	switch {
	case len(setAside) == 0:
	case len(kept) == 0:
		delete(object, "promoTypes")
	default:
		object["promoTypes"] = kept
	}
	return setAside
}

// refusal is the rule of Check a token breaks, "" for none.
func refusal(token, rarity string, finishes, published []string) string {
	switch {
	case !slugRe.MatchString(token):
		return "not a slug"
	case len(token) > TokenLimit && holdsASet(token, published):
		return fmt.Sprintf("longer than %d and holding a set's name", TokenLimit)
	case rarity != "" && token == rarity:
		return "says the rarity"
	}
	for _, finish := range finishes {
		if finish != "" && token == finish {
			return "says the finish"
		}
	}
	return ""
}

// publishedSlugs are the set names a long token is held against, as slugs.
// A name of three letters or fewer is a code that a word happens to contain.
func publishedSlugs(sets []string) []string {
	published := make([]string, 0, len(sets))
	for _, name := range sets {
		if slug := Slug(name); len(slug) > 3 {
			published = append(published, slug)
		}
	}
	return published
}

// FitInto runs Fit on a builder's encoded output in place.
func FitInto(buf *bytes.Buffer) ([]string, error) {
	fitted, setAside, err := Fit(buf.Bytes())
	if err != nil || len(setAside) == 0 {
		return nil, err
	}
	buf.Reset()
	_, _ = buf.Write(fitted)
	return setAside, nil
}
