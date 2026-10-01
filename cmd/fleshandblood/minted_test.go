package main

import (
	"slices"
	"testing"
)

// TestDropMintedTwinsKeepsTheSoldCard pins that a minted entry is dropped
// when a priced entry wears its fabId, or its set, padding-blind number and
// name, and kept otherwise.
func TestDropMintedTwinsKeepsTheSoldCard(t *testing.T) {
	entry := func(id, setCode, number, name, fabID string, product int) map[string]any {
		links := map[string]any{"fabId": fabID}
		if product != 0 {
			links["tcgPlayerId"] = product
		}
		return map[string]any{"id": id, "setCode": setCode, "number": number, "name": name, "externalLinks": links}
	}
	kept, dropped := dropMintedTwins([]any{
		entry("her0156", "HER", "HER0156", "Ser Boltyn", "HER156", 500),
		entry("m-her156", "HER", "HER156", "ser boltyn", "OTHER", 0),
		entry("m-hnt134", "HNT", "HNT134", "Knife Through Butter", "HER156", 0),
		entry("m-token", "HNT", "HNT200", "Gold", "HNT200", 0),
	})
	var ids []string
	for _, raw := range kept {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	if !slices.Equal(ids, []string{"her0156", "m-token"}) || len(dropped) != 2 {
		t.Errorf("kept %v, dropped %q; want the two twins dropped", ids, dropped)
	}
}
