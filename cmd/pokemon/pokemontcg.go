package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

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

// pokemontcgClient bounds every pokemontcg.io call, the way tcgdexClient
// bounds tcgdex's: without a deadline a host that has stopped answering
// holds the build for the platform's dial timeout.
var pokemontcgClient = &http.Client{Timeout: 30 * time.Second}

// fetchPokemontcgSets asks the live API, retried the way tcgdex is. One
// attempt was losing the symbols to blips that clear on their own: three
// consecutive requests one afternoon answered 500, then 502, then 200. A
// status the server will give again on a retry is not retried.
func fetchPokemontcgSets() ([]byte, error) {
	var lastErr error
	for attempt, wait := 0, 2*time.Second; attempt < 4; attempt, wait = attempt+1, wait*2 {
		if attempt > 0 {
			log.Printf("pokemontcg.io: %v; retrying in %v", lastErr, wait)
			time.Sleep(wait)
		}
		req, err := http.NewRequest(http.MethodGet, pokemontcgSetsURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "datastore-gen/1.0 (+https://github.com/mtgban/datastore-gen)")
		resp, err := pokemontcgClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: HTTP %d", pokemontcgSetsURL, resp.StatusCode)
			// A client-side status will not change on a retry.
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return nil, lastErr
			}
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

// loadPokemontcgSets reads the set list from a path, or from the live API
// behind the last-good cache. A failure is reported and not fatal: this
// fills the symbol of 20 sets the other 255 do not need, and no build is
// worth losing over it. What the cache buys is that those 20 keep the
// symbol they had last time instead of silently going blank.
func loadPokemontcgSets(path, cacheDir string) ([]pokemontcgSet, error) {
	var data []byte
	var err error
	if path != "" {
		data, err = os.ReadFile(path)
	} else {
		data, err = cachedFetch("pokemontcg.io", cacheDir, "pokemontcg-sets.json", fetchPokemontcgSets, readsAsSets)
	}
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
