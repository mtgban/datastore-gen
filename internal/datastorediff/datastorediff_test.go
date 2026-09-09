package datastorediff

import "testing"

func doc(cards, sets, sealed string) []byte {
	return []byte(`{"game":"test","cards":` + cards + `,"sets":` + sets + `,"sealed":` + sealed + `}`)
}

const noSets, noSealed = `{}`, `[]`

func compare(t *testing.T, before, after []byte) Change {
	t.Helper()
	c, err := Compare(before, after)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	return c
}

// TestUnchangedIsEmpty pins the case that decides most of the history: a
// commit that leaves the datastore alone must report nothing, or every
// refactor gets a tag saying it published something.
func TestUnchangedIsEmpty(t *testing.T) {
	d := doc(`[{"id":"a","name":"A"}]`, noSets, noSealed)
	c := compare(t, d, d)
	if !c.Empty() {
		t.Errorf("Empty() = false for identical documents: %s", c)
	}
	if got := c.String(); got != "no change" {
		t.Errorf("String() = %q, want %q", got, "no change")
	}
}

// TestFieldPublishedIsNotRewording is the distinction the whole package
// exists for. A commit that starts publishing a field and one that reworded
// a field already published both change the same entries, and only the
// second is a rewording.
func TestFieldPublishedIsNotRewording(t *testing.T) {
	before := doc(`[{"id":"a","name":"A"},{"id":"b","name":"B"}]`, noSets, noSealed)
	published := doc(`[{"id":"a","name":"A","language":"en"},{"id":"b","name":"B","language":"en"}]`, noSets, noSealed)
	reworded := doc(`[{"id":"a","name":"Ay"},{"id":"b","name":"Bee"}]`, noSets, noSealed)

	c := compare(t, before, published)
	if c.FieldsAdded["language"] != 2 {
		t.Errorf("published: FieldsAdded[language] = %d, want 2", c.FieldsAdded["language"])
	}
	if len(c.ValuesChanged) != 0 {
		t.Errorf("published: reported %v as reworded", c.ValuesChanged)
	}
	if got, want := c.String(), "published language on 2"; got != want {
		t.Errorf("published: String() = %q, want %q", got, want)
	}

	c = compare(t, before, reworded)
	if c.ValuesChanged["name"] != 2 {
		t.Errorf("reworded: ValuesChanged[name] = %d, want 2", c.ValuesChanged["name"])
	}
	if len(c.FieldsAdded) != 0 {
		t.Errorf("reworded: reported %v as published", c.FieldsAdded)
	}
}

// TestFieldMoving pins a real commit's shape: a builder that starts
// publishing the language stops carrying it as a promo type, and the
// summary has to show both halves or it reads as an unexplained rewording.
func TestFieldMoving(t *testing.T) {
	before := doc(`[{"id":"a","promoTypes":["japanese"]}]`, noSets, noSealed)
	after := doc(`[{"id":"a","language":"ja"}]`, noSets, noSealed)
	c := compare(t, before, after)
	if c.FieldsAdded["language"] != 1 || c.FieldsRemoved["promoTypes"] != 1 {
		t.Errorf("got %+v, want language published once and promoTypes dropped once", c)
	}
	if got, want := c.String(), "published language on 1; dropped promoTypes on 1"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestNewEntriesDoNotCountAsPublishedFields guards the obvious wrong
// implementation: a card arriving carries every field it has, and counting
// those would report a handful of new cards as the whole schema changing.
func TestNewEntriesDoNotCountAsPublishedFields(t *testing.T) {
	before := doc(`[{"id":"a","name":"A"}]`, noSets, noSealed)
	after := doc(`[{"id":"a","name":"A"},{"id":"b","name":"B","rarity":"Rare"}]`, noSets, noSealed)
	c := compare(t, before, after)
	if c.IDsAdded != 1 {
		t.Errorf("IDsAdded = %d, want 1", c.IDsAdded)
	}
	if len(c.FieldsAdded) != 0 {
		t.Errorf("the new entry's fields were counted as published: %v", c.FieldsAdded)
	}
	if got, want := c.String(), "ids +1/-0"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestSetsAndSealed pins the two counted whole. A set folding into another
// moves the total by a fraction of a percent while emptying a set, which is
// why the count is reported rather than inferred from the cards.
func TestSetsAndSealed(t *testing.T) {
	before := doc(`[]`, `{"AAA":{"name":"A"},"BBB":{"name":"B"}}`, `[{"id":"s1"}]`)
	fewer := doc(`[]`, `{"AAA":{"name":"A"}}`, `[{"id":"s1"}]`)
	if got, want := compare(t, before, fewer).String(), "sets 2->1"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	reworded := doc(`[]`, `{"AAA":{"name":"A","promotional":true},"BBB":{"name":"B"}}`, `[{"id":"s1"}]`)
	if got, want := compare(t, before, reworded).String(), "1 sets reworded"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	resealed := doc(`[]`, `{"AAA":{"name":"A"},"BBB":{"name":"B"}}`, `[{"id":"s1","name":"box"}]`)
	if got, want := compare(t, before, resealed).String(), "sealed reworded"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestGalleryCardsAreFound is Riftbound's shape: the builder publishes the
// card gallery Riot serves its own site, so the cards sit under the page's
// blades rather than at the top. Read as an unkeyed document it reported
// thousands of leaves moving for what is a handful of cards arriving;
// internal/vocabulary walks the same path for the same reason.
func TestGalleryCardsAreFound(t *testing.T) {
	gallery := func(cards string) []byte {
		return []byte(`{"pageProps":{"page":{"blades":[{"cards":{"items":` + cards + `}}]}}}`)
	}
	before := gallery(`[{"publicCode":"OGN-001","rarity":"Common"}]`)
	after := gallery(`[{"publicCode":"OGN-001","rarity":"Common"},{"publicCode":"OGN-002","rarity":"Rare"}]`)
	c := compare(t, before, after)
	if c.ByLeaves {
		t.Fatal("fell back to leaves; the gallery cards were not found")
	}
	if got, want := c.String(), "ids +1/-0"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	// And a field published on a gallery card reads like anyone else's.
	c = compare(t, before, gallery(`[{"publicCode":"OGN-001","rarity":"Common","art":"x"}]`))
	if c.FieldsAdded["art"] != 1 {
		t.Errorf("FieldsAdded = %v, want art on 1", c.FieldsAdded)
	}
}

// TestNoCardsKeyFallsBackToLeaves is a shape with cards nowhere this knows
// to look. A document that
// publishes the upstream payload has no cards key to compare, and reporting
// it as unchanged - which keying on a missing field does - hid real changes
// behind "no material change" until this fell back instead.
func TestNoCardsKeyFallsBackToLeaves(t *testing.T) {
	before := []byte(`{"pageProps":{"blades":[{"publicCode":"OGN-001","rarity":"Common"}]}}`)
	after := []byte(`{"pageProps":{"blades":[{"publicCode":"OGN-001","rarity":"Rare","art":"x"}]}}`)
	c := compare(t, before, after)
	if c.Empty() {
		t.Fatal("Empty() = true for a document that changed")
	}
	if !c.ByLeaves {
		t.Error("ByLeaves = false; the fallback did not run")
	}
	if c.LeavesAdded != 1 || c.LeavesChanged != 1 {
		t.Errorf("got +%d/~%d, want one leaf added and one changed", c.LeavesAdded, c.LeavesChanged)
	}
}

// TestUnkeyedEntriesStillCount covers the entries a game leaves unnumbered:
// they carry no uuid, so they are keyed by position, and one arriving still
// moves the count.
func TestUnkeyedEntriesStillCount(t *testing.T) {
	before := doc(`[{"name":"A"}]`, noSets, noSealed)
	after := doc(`[{"name":"A"},{"name":"B"}]`, noSets, noSealed)
	if got := compare(t, before, after).IDsAdded; got != 1 {
		t.Errorf("IDsAdded = %d, want 1", got)
	}
}

func TestMalformedInputIsAnError(t *testing.T) {
	if _, err := Compare([]byte(`{`), []byte(`{}`)); err == nil {
		t.Error("Compare accepted a truncated document")
	}
}
