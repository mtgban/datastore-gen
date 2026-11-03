package main

import (
	"encoding/json"
	"strings"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// pokemontcgSetsURL is pokemontcg.io's set list, read for the one field
// tcgdex leaves empty on 49 of its 218 sets: the symbol. Every one of its
// 174 sets carries one.
const pokemontcgSetsURL = "https://api.pokemontcg.io/v2/sets?pageSize=250"

// pokemontcgSet is the slice of a pokemontcg.io set this build reads.
type pokemontcgSet struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PtcgoCode   string `json:"ptcgoCode"`
	ReleaseDate string `json:"releaseDate"`
	Images      struct {
		Symbol string `json:"symbol"`
	} `json:"images"`
}

// loadPokemontcgSets reads the set list from a path, or from the live API,
// retried and behind the last-good cache: pokemontcg.io answers 5xx often
// enough that a run can lose it outright. A failure is reported and not fatal: this
// fills the symbol of 20 sets the other 255 do not need, and no build is
// worth losing over it. What the cache buys is that those 20 keep the
// symbol they had last time instead of silently going blank.
func loadPokemontcgSets(path, cacheDir string) ([]pokemontcgSet, error) {
	location := path
	if location == "" {
		location = pokemontcgSetsURL
	}
	data, err := emit.Upstream{
		Source:    "pokemontcg.io",
		Check:     readsAsSets,
		CacheDir:  cacheDir,
		CacheName: "pokemontcg-sets.json",
	}.Fetch(location)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []pokemontcgSet `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return emit.DropRepeats("pokemontcg.io sets", payload.Data, func(s pokemontcgSet) string { return s.ID }), nil
}

// pokemontcgDate spells a pokemontcg.io release date the way this datastore
// spells one: it writes "1999/01/09" where every date here is "1999-01-09".
func pokemontcgDate(date string) string {
	return strings.ReplaceAll(date, "/", "-")
}
