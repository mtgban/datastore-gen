package main

import (
	"bytes"
	"encoding/json"
	"testing"

	tcgplayer "github.com/mtgban/go-tcgplayer"

	"github.com/mtgban/datastore-gen/internal/emit"
)

// TestDonCardPublishesNoColor pins that a DON!! card, which the catalog
// gives no Color, is encoded with no color key at all rather than an empty
// one, while a coloured card keeps both forms.
func TestDonCardPublishesNoColor(t *testing.T) {
	// 434340 as the catalog lists it, cut to what the entry reads.
	var don, red tcgplayer.Product
	for _, decode := range []struct {
		raw  string
		into *tcgplayer.Product
	}{
		{`{"productId": 434340, "name": "DON!! Card", "groupId": 17659, "extendedData": [
			{"name": "Rarity", "value": "DON!!"},
			{"name": "Description", "value": "Your Turn +1000"},
			{"name": "CardType", "value": "DON!!"}]}`, &don},
		{`{"productId": 600000, "name": "Monkey.D.Luffy - ST01-001", "groupId": 17659, "extendedData": [
			{"name": "Rarity", "value": "L"},
			{"name": "CardType", "value": "Leader"},
			{"name": "Color", "value": "Red"}]}`, &red},
	} {
		if err := json.Unmarshal([]byte(decode.raw), decode.into); err != nil {
			t.Fatal(err)
		}
	}
	doc := &emit.Datastore{Cards: []*emit.Card{
		singleEntry(single{product: don, number: "DON", baseName: "DON!! Card"}, "ST-01-PRE", "Normal", ""),
		singleEntry(single{product: red, number: "ST01-001", baseName: "Monkey.D.Luffy"}, "ST-01", "Normal", ""),
	}}
	doc.Lists()

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(emit.Envelope("2026-11-03", doc)); err != nil {
		t.Fatal(err)
	}
	data, err := emit.Unwrap(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		Cards []map[string]json.RawMessage `json:"cards"`
	}
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatal(err)
	}
	if len(read.Cards) != len(doc.Cards) {
		t.Fatalf("read back %d cards, want %d", len(read.Cards), len(doc.Cards))
	}
	for i, want := range []bool{false, true} {
		card := read.Cards[i]
		for _, key := range []string{"color", "colors"} {
			if _, has := card[key]; has != want {
				t.Errorf("%s: %s present = %v, want %v (%s)", card["id"], key, has, want, card[key])
			}
		}
	}
}
