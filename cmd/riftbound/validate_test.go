package main

import (
	"strings"
	"testing"

	"github.com/mtgban/datastore-gen/internal/validate"
)

// TestSharedChecksReadRiftbound pins that the gallery, read one printing at a
// time, is held to the shared checks: each case breaks one thing in a file
// that otherwise passes.
func TestSharedChecksReadRiftbound(t *testing.T) {
	const (
		jinx = `{"id":"ogn-001-298","name":"Jinx","publicCode":"OGN-001/298","number":"1","setCode":"OGN","externalLinks":{"tcgPlayerId":100},` +
			`"printings":[{"finish":"Normal","id":"ogn-001-298"},{"finish":"Foil","id":"ogn-001-298_foil"}]}`
		box = `{"id":"ogn-box","name":"Booster Box","setCode":"OGN","externalLinks":{"tcgPlayerId":900}}`
	)
	want := map[int][]string{100: {"Foil", "Normal"}}
	file := func(blade, cards, sealed string) []byte {
		return []byte(`{"meta":{"date":"2026-10-01","version":"1"},"data":{"pageProps":{"page":{"blades":[{"type":"` + blade +
			`","sets":{"items":[{"id":"OGN","name":"Origins"}]},"cards":{"items":[` + cards + `]},"sealed":{"items":[` + sealed + `]}}]}}}}`)
	}
	for _, test := range []struct {
		desc, data, refused string
	}{
		{"a clean gallery", string(file("riftboundCardGallery", jinx, box)), ""},
		{"no card gallery", string(file("somethingElse", jinx, box)), "no card gallery blade"},
		{"a card in a set the file lacks", string(file("riftboundCardGallery", strings.Replace(jinx, `"setCode":"OGN"`, `"setCode":"SFD"`, 1), box)), "unknown set SFD"},
		{"a finish the skus do not sell", string(file("riftboundCardGallery", strings.Replace(jinx, `,{"finish":"Foil","id":"ogn-001-298_foil"}`, ``, 1), box)), "skus carry [Foil Normal]"},
		{"a number with a space", string(file("riftboundCardGallery", strings.Replace(jinx, `"number":"1"`, `"number":"1 a"`, 1), box)), "collector number a query cannot carry"},
		{"a sealed id a printing wears", string(file("riftboundCardGallery", jinx, strings.Replace(box, "ogn-box", "ogn-001-298", 1))), "duplicate id ogn-001-298"},
	} {
		document, _, _, _, _, err := readDocument([]byte(test.data))
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
