package vocabulary

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
)

func encodeDocument(t *testing.T, document map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(emit.Envelope("2026-10-01", document)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestFitSetsAsideWhatCheckRefuses pins that a token Check would refuse
// comes off the one printing wearing it, and nothing else moves: the long
// token holding a set's name that stopped One Piece's publish on
// 2026-09-30, a rarity echo, and a finish echo on a printing.
func TestFitSetsAsideWhatCheckRefuses(t *testing.T) {
	document := map[string]any{
		"sets": map[string]any{"ST11": map[string]any{"name": "Starter Deck 11: Uta"}},
		"cards": []any{
			map[string]any{"id": "st11-001_1", "rarity": "Leader", "finish": "Normal",
				"promoTypes": []any{"starterdeck11utadeckbattle", "deckbattle"}},
			map[string]any{"id": "st11-002_2", "rarity": "Secret Rare", "promoTypes": []any{"secretrare"}},
			map[string]any{"id": "1951", "rarity": "Rare", "promoTypes": []any{"d23"},
				"printings": []any{
					map[string]any{"id": "1951_coldfoil", "finish": "Cold Foil", "promoTypes": []any{"coldfoil", "satin"}},
				}},
		},
	}
	fitted, setAside, err := Fit(encodeDocument(t, document))
	if err != nil {
		t.Fatal(err)
	}
	if len(setAside) != 3 {
		t.Fatalf("set aside %d tokens, want 3: %q", len(setAside), setAside)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "onepiece.json")
	if err := os.WriteFile(path, fitted, 0o600); err != nil {
		t.Fatal(err)
	}
	printings, err := ReadDatastore(path)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := SetNames(path)
	if err != nil {
		t.Fatal(err)
	}
	problems := Check(printings, sets)
	if len(problems.NotSlugs)+len(problems.TooLong)+len(problems.RarityEchoes)+len(problems.FinishEchoes) > 0 {
		t.Errorf("Check still refuses the fitted document: %q", problems.Lines())
	}
	want := map[string][]string{
		"st11-001_1":    {"deckbattle"},
		"st11-002_2":    nil,
		"1951_coldfoil": {"d23", "satin"},
	}
	for _, printing := range printings {
		if tokens, listed := want[printing.ID]; listed && !slices.Equal(printing.PromoTypes, tokens) {
			t.Errorf("%s keeps %q, want %q", printing.ID, printing.PromoTypes, tokens)
		}
	}
}

// TestFitLeavesACleanDocumentAlone pins that a build with nothing to set
// aside publishes the bytes it encoded, so Fit changes no ordinary build.
func TestFitLeavesACleanDocumentAlone(t *testing.T) {
	encoded := encodeDocument(t, map[string]any{
		"sets":  map[string]any{"OP01": map[string]any{"name": "Romance Dawn"}},
		"cards": []any{map[string]any{"id": "op01-001_1", "rarity": "Leader", "promoTypes": []any{"prerelease"}}},
	})
	fitted, setAside, err := Fit(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(setAside) != 0 || !bytes.Equal(fitted, encoded) {
		t.Errorf("Fit changed a clean document: set aside %q", setAside)
	}
}
