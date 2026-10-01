package main

import (
	"strings"
	"testing"

	"github.com/mtgban/datastore-gen/internal/validate"
)

// TestSharedChecksReadLorcana pins that a Lorcana file, read one printing at
// a time, is held to the shared checks: each case breaks one thing in a
// file that otherwise passes.
func TestSharedChecksReadLorcana(t *testing.T) {
	const (
		ariel = `{"id":1,"fullName":"Ariel - On Human Legs","setCode":"1","number":"1","image":"https://example.test/1.png","externalLinks":{"tcgPlayerId":100},` +
			`"printings":[{"finish":"Normal","id":"1"},{"finish":"Cold Foil","id":"1_coldfoil"}]}`
		panorama = `{"id":2,"fullName":"Dopey - Drawn to Music","setCode":"1","number":"2","image":"https://example.test/2.png","externalLinks":{"tcgPlayerId":200,"tcgPlayerExtraIds":[201]},` +
			`"printings":[{"finish":"Normal","id":"2"},{"finish":"Cold Foil","id":"2_coldfoil"}]}`
		box = `{"id":"tfc-box","name":"Booster Box","setCode":"1","externalLinks":{"tcgPlayerId":900}}`
	)
	want := map[int][]string{100: {"Cold Foil", "Normal"}, 200: {"Normal"}, 201: {"Cold Foil"}}
	file := func(cards, sealed string) []byte {
		return []byte(`{"meta":{"date":"2026-10-01","version":"1"},"data":{"sets":{"1":{"name":"The First Chapter"}},"cards":[` +
			cards + `],"sealed":[` + sealed + `]}}`)
	}
	for _, test := range []struct {
		desc, data, refused string
	}{
		{"a clean file, a foil sold as a product of its own", string(file(ariel+","+panorama, box)), ""},
		{"a uuid a query cannot carry", string(file(strings.Replace(ariel, `"1_coldfoil"`, `"1 coldfoil"`, 1)+","+panorama, box)), "a uuid nothing can carry"},
		{"a number with a space", string(file(strings.Replace(ariel, `"number":"1"`, `"number":"1 a"`, 1)+","+panorama, box)), "collector number a query cannot carry"},
		{"a printing with no finish", string(file(strings.Replace(ariel, `"finish":"Normal",`, ``, 1)+","+panorama, box)), "missing identity"},
		{"a finish printed twice", string(file(strings.Replace(ariel, `"finish":"Cold Foil"`, `"finish":"Normal"`, 1)+","+panorama, box)), `carries finish "Normal" twice`},
		{"a finish the skus do not sell", string(file(strings.Replace(ariel, `,{"finish":"Cold Foil","id":"1_coldfoil"}`, ``, 1)+","+panorama, box)), "skus carry [Cold Foil Normal]"},
		{"a sealed id a query cannot carry", string(file(ariel+","+panorama, strings.Replace(box, "tfc-box", "tfc box", 1))), "a uuid nothing can carry"},
		{"a card in a set the file lacks", string(file(strings.Replace(ariel, `"setCode":"1"`, `"setCode":"9"`, 1)+","+panorama, box)), "unknown set 9"},
	} {
		document, _, err := readDocument([]byte(test.data), want)
		if err == nil {
			_, err = validate.Check(document, want, validationRules())
		}
		switch {
		case test.refused == "" && err != nil:
			t.Errorf("%s: refused: %v", test.desc, err)
		case test.refused != "" && (err == nil || !strings.Contains(err.Error(), test.refused)):
			t.Errorf("%s: %v, want a refusal naming %q", test.desc, err, test.refused)
		}
	}
}

// TestReadDocumentSaysAForeignClaimOutLoud pins that a product upstream links
// and the catalog types as no card prices nothing rather than stopping the
// build: the dump can lag a day behind upstream.
func TestReadDocumentSaysAForeignClaimOutLoud(t *testing.T) {
	data := []byte(`{"meta":{"date":"2026-10-01","version":"1"},"data":{"sets":{"1":{"name":"The First Chapter"}},"cards":[` +
		`{"id":1,"fullName":"Ariel - On Human Legs","setCode":"1","number":"1","externalLinks":{"tcgPlayerId":555},"printings":[{"finish":"Normal","id":"1"}]}` +
		`],"sealed":[]}}`)
	want := map[int][]string{}
	document, counted, err := readDocument(data, want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validate.Check(document, want, validationRules()); err != nil || counted.carried != 0 {
		t.Errorf("Check = %v, carried %d; want it published carrying nothing", err, counted.carried)
	}
}
