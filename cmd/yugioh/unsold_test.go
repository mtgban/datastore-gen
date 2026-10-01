package main

import (
	"slices"
	"testing"
)

// TestDropUnsoldKeepsTheEuropeanPrints pins that an entry naming no product
// is kept only when it is a European first print carrying its passcode.
func TestDropUnsoldKeepsTheEuropeanPrints(t *testing.T) {
	entry := func(id, number string, product, passcode int) map[string]any {
		links := map[string]any{}
		if product != 0 {
			links["tcgPlayerId"] = product
		}
		if passcode != 0 {
			links["konamiId"] = passcode
		}
		return map[string]any{"id": id, "number": number, "name": "Dark Magician", "externalLinks": links}
	}
	kept, dropped := dropUnsold([]any{
		entry("lob-en005", "LOB-EN005", 100, 46986414),
		entry("lob-e005", "LOB-E005", 0, 46986414),
		entry("lob-e006", "LOB-E006", 0, 0),
		entry("lob-005", "LOB-005", 0, 46986414),
	})
	var ids []string
	for _, raw := range kept {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	if !slices.Equal(ids, []string{"lob-en005", "lob-e005"}) || len(dropped) != 2 {
		t.Errorf("kept %v, dropped %q; want the passcodeless and the non-European dropped", ids, dropped)
	}
}
