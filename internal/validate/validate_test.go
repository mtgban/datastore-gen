package validate

import (
	"strings"
	"testing"
)

func envelope(payload string) []byte {
	return []byte(`{"meta":{"date":"2026-10-01","version":"1"},"data":` + payload + `}`)
}

const (
	sets   = `"sets":{"AB":{"name":"Alpha"}}`
	priced = `{"id":"ab1_100","name":"Ace","number":"1","setCode":"AB","rarity":"R","finish":"Normal","externalLinks":{"tcgPlayerId":100}}`
	foil   = `{"id":"ab1_100_foil","name":"Ace","number":"1","setCode":"AB","rarity":"R","finish":"Foil","externalLinks":{"tcgPlayerId":100}}`
	sealed = `"sealed":[{"id":"ab-900","name":"Box","setCode":"AB","externalLinks":{"tcgPlayerId":900}}]`
)

var rules = Rules{Game: "test", Identity: []string{"name", "number", "setCode", "rarity"}}

// TestDatastoreRefusesWhatAConsumerWouldMisread pins each shared check on a
// document that passes but for the one thing named.
func TestDatastoreRefusesWhatAConsumerWouldMisread(t *testing.T) {
	want := map[int][]string{100: {"Normal", "Foil"}}
	doc := func(cards, extra string) []byte {
		return envelope(`{"game":"test",` + sets + `,"cards":[` + cards + `],` + sealed + extra + `}`)
	}
	for _, test := range []struct {
		desc, data, refused string
	}{
		{"a clean file", string(doc(priced+","+foil, "")), ""},
		{"another game's", strings.Replace(string(doc(priced+","+foil, "")), `"game":"test"`, `"game":"other"`, 1), `game is "other"`},
		{"an id used twice", string(doc(priced+","+foil+","+priced, "")), "duplicate card id ab1_100"},
		{"an id a query cannot carry", string(doc(strings.Replace(priced, "ab1_100", "ab1 100", 1)+","+foil, "")), "a uuid nothing can carry"},
		{"a number with a space", string(doc(strings.Replace(priced, `"number":"1"`, `"number":"1 a"`, 1)+","+foil, "")), "collector number a query cannot carry"},
		{"a card in a set the file lacks", string(doc(strings.Replace(priced, `"setCode":"AB"`, `"setCode":"ZZ"`, 1)+","+foil, "")), "unknown set ZZ"},
		{"a finish emitted twice", string(doc(priced+","+strings.Replace(priced, "ab1_100", "ab1_100_x", 1)+","+foil, "")), `carries finish "Normal" twice`},
		{"a finish the skus do not sell", string(doc(priced, "")), "emits finishes [Normal], skus carry [Foil Normal]"},
		{"no finish", string(doc(strings.Replace(priced, `"finish":"Normal",`, "", 1)+","+foil, "")), "missing identity"},
	} {
		_, err := Datastore([]byte(test.data), want, rules)
		switch {
		case test.refused == "" && err != nil:
			t.Errorf("%s: refused: %v", test.desc, err)
		case test.refused != "" && (err == nil || !strings.Contains(err.Error(), test.refused)):
			t.Errorf("%s: %v, want a refusal naming %q", test.desc, err, test.refused)
		}
	}
}

// TestDatastoreRunsTheGamesRules pins that a game's own check, its minted
// key and its final check are run, and that Counts is what the file holds.
func TestDatastoreRunsTheGamesRules(t *testing.T) {
	data := envelope(`{"game":"test",` + sets + `,"cards":[` + priced + `,` + foil + `],` + sealed + `}`)
	want := map[int][]string{100: {"Normal", "Foil"}}
	counts, err := Datastore(data, want, rules)
	if err != nil || counts != (Counts{Sets: 1, Cards: 2, Sealed: 1}) {
		t.Fatalf("Datastore = %+v, %v", counts, err)
	}
	refusing := rules
	refusing.Check = func(card Card) error {
		if card.Field("rarity") == "R" {
			return errText("rarity R refused")
		}
		return nil
	}
	if _, err := Datastore(data, want, refusing); err == nil || err.Error() != "rarity R refused" {
		t.Errorf("Check not run: %v", err)
	}
	final := rules
	final.Finally = func(cards []Card, sealed []Sealed, sets map[string]string) error {
		if len(cards) != 2 || len(sealed) != 1 || sets["AB"] != "Alpha" {
			return errText("Finally saw the wrong file")
		}
		return errText("final check ran")
	}
	if _, err := Datastore(data, want, final); err == nil || err.Error() != "final check ran" {
		t.Errorf("Finally: %v", err)
	}
}

type errText string

func (e errText) Error() string { return string(e) }
