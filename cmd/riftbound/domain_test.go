package main

import (
	"slices"
	"testing"

	"github.com/mtgban/go-tcgplayer"
)

// galleryRow is a gallery card row carrying a name and its domain ids.
func galleryRow(name string, ids ...string) any {
	var values []any
	for _, id := range ids {
		values = append(values, map[string]any{"id": id, "label": id})
	}
	return map[string]any{"name": name, "domain": map[string]any{"label": "Domain", "values": values}}
}

func domainIDs(item map[string]any) []string {
	domain, _ := item["domain"].(map[string]any)
	values, _ := domain["values"].([]any)
	var ids []string
	for _, v := range values {
		ids = append(ids, v.(map[string]any)["id"].(string))
	}
	return ids
}

// TestDomainsStamp pins where an adopted or minted printing's domain comes
// from: the gallery's own printing of the card first, the catalog after it,
// and nothing rather than a domain the gallery has never shown.
func TestDomainsStamp(t *testing.T) {
	gallery := []any{
		galleryRow("Fury Rune", "fury"),
		galleryRow("Fury Rune", "fury"),
		galleryRow("Ahri", "calm"),
		galleryRow("Ahri", "mind"),
		galleryRow("Abandoned Hall", "colorless"),
		galleryRow("Jinx", "chaos"),
	}
	for _, test := range []struct {
		desc, name, catalog string
		want                []string
	}{
		{"a gallery printing of the name answers first", "Fury Rune", "Calm", []string{"fury"}},
		{"two cards under one name leave it to the catalog", "Ahri", "Mind", []string{"mind"}},
		{"the catalog answers for a name the gallery spells otherwise", "Ahri, Alluring", "Calm", []string{"calm"}},
		{"in its own order", "Jinx, Rebel", "Fury;Chaos", []string{"fury", "chaos"}},
		{"its None is the gallery's colorless", "Altar of Blood", "None", []string{"colorless"}},
		{"a domain the gallery never shows is not invented", "Mystery", "Fury;Starlight", nil},
		{"and a catalog saying nothing leaves none", "Sprite // Gold", "", nil},
	} {
		d := domainsOf(gallery)
		item := map[string]any{"name": test.name}
		product := tcgplayer.Product{Name: test.name}
		if test.catalog != "" {
			product.ExtendedData = []tcgplayer.ExtendedField{{Name: "Domain", Value: test.catalog}}
		}
		d.stamp(item, product)
		got := domainIDs(item)
		if !slices.Equal(got, test.want) {
			t.Errorf("%s: %q took %v, want %v", test.desc, test.name, got, test.want)
		}
	}
}
