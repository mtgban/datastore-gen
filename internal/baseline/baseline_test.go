package baseline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtgban/datastore-gen/internal/emit"
)

func counts(cards, sealed int, bySet map[string]int) Counts {
	return Counts{Cards: cards, Sealed: sealed, BySet: bySet}
}

// TestRegressionRefusesTheThreeShapes pins what is refused and what is
// merely logged: a total past the tolerance, a set emptied, a set halved.
func TestRegressionRefusesTheThreeShapes(t *testing.T) {
	previous := counts(1000, 100, map[string]int{"A": 500, "B": 300, "C": 200})
	for _, test := range []struct {
		desc    string
		current Counts
		refused string
	}{
		{"the same build", previous, ""},
		{"growth", counts(1100, 120, map[string]int{"A": 550, "B": 330, "C": 220}), ""},
		{"a drop inside the tolerance", counts(995, 100, map[string]int{"A": 495, "B": 300, "C": 200}), ""},
		{"a total past the tolerance", counts(980, 100, map[string]int{"A": 480, "B": 300, "C": 200}), "980 cards, down from 1000"},
		{"sealed past the tolerance", counts(1000, 85, map[string]int{"A": 500, "B": 300, "C": 200}), "85 sealed products"},
		{"a set emptied under the tolerance", counts(995, 100, map[string]int{"A": 500, "B": 300, "D": 195}), "hold no card any more: C"},
		{"a set halved", counts(995, 100, map[string]int{"A": 500, "B": 300, "C": 99, "D": 96}), "lost more than half"},
		{"a set at exactly half is logged, not refused", counts(1000, 100, map[string]int{"A": 500, "B": 300, "C": 100, "D": 100}), ""},
	} {
		err := Regression(previous, test.current, 0.01)
		switch {
		case test.refused == "" && err != nil:
			t.Errorf("%s: refused: %v", test.desc, err)
		case test.refused != "" && err == nil:
			t.Errorf("%s: accepted, want a refusal naming %q", test.desc, test.refused)
		case test.refused != "" && !strings.Contains(err.Error(), test.refused):
			t.Errorf("%s: refused for %q, want %q", test.desc, err, test.refused)
		}
	}
}

// TestRegressionReadsAFloorBesideTheTolerance pins that a loss of MinLoss
// entries or fewer is never refused, however small the game: Palworld's
// 279 cards lost 7 minted ones to an upstream refiling on 2026-09-22.
func TestRegressionReadsAFloorBesideTheTolerance(t *testing.T) {
	previous := counts(279, 12, map[string]int{"PW": 279})
	if err := Regression(previous, counts(272, 12, map[string]int{"PW": 272}), 0.01); err != nil {
		t.Errorf("7 of 279 lost: refused: %v", err)
	}
	if err := Regression(previous, counts(268, 12, map[string]int{"PW": 268}), 0.01); err == nil {
		t.Error("11 of 279 lost: accepted, want a refusal")
	}
}

// TestRegressionTellsAMoveFromALoss pins that a set whose cards the build
// still carries under another set is logged, not refused - TCGplayer
// renaming a set, a builder filing a promo by its catalog group (Lorcana's
// D23 on 2026-09-25) - while a set whose cards are gone is refused as ever.
func TestRegressionTellsAMoveFromALoss(t *testing.T) {
	where := func(pairs ...string) map[string]string {
		out := map[string]string{}
		for i := 0; i < len(pairs); i += 2 {
			out[pairs[i]] = pairs[i+1]
		}
		return out
	}
	previous := Counts{Cards: 4, BySet: map[string]int{"11": 2, "D23": 2},
		Where: where("tcg:1:", "11", "tcg:2:", "11", "tcg:3:", "D23", "tcg:4:", "D23")}
	moved := Counts{Cards: 4, BySet: map[string]int{"11": 4},
		Where: where("tcg:1:", "11", "tcg:2:", "11", "tcg:3:", "11", "tcg:4:", "11")}
	if err := Regression(previous, moved, 0.01); err != nil {
		t.Errorf("a set moved whole: refused: %v", err)
	}
	gone := Counts{Cards: 4, BySet: map[string]int{"11": 2, "X": 2},
		Where: where("tcg:1:", "11", "tcg:2:", "11", "tcg:5:", "X", "tcg:6:", "X")}
	if err := Regression(previous, gone, 0.01); err == nil || !strings.Contains(err.Error(), "hold no card any more: D23") {
		t.Errorf("a set lost whole: %v, want a refusal naming D23", err)
	}
	halved := Counts{Cards: 4, BySet: map[string]int{"11": 3, "X": 1},
		Where: where("tcg:1:", "11", "tcg:2:", "11", "tcg:3:", "11", "tcg:5:", "X")}
	if err := Regression(previous, halved, 0.01); err != nil {
		t.Errorf("half a set moved, half lost: refused: %v", err)
	}
}

// TestRegressionWithoutABaseline pins that a first build, measured against
// nothing, is not refused for anything.
func TestRegressionWithoutABaseline(t *testing.T) {
	if err := Regression(Counts{}, counts(1, 1, map[string]int{"A": 1}), 0.01); err != nil {
		t.Errorf("Regression against an empty baseline = %v, want nil", err)
	}
}

// TestCountRefusesABareDocument pins that a baseline is read only in the
// envelope: every one on the bucket has been enveloped since 2026-09-24, and
// a bare one read as the document would be measured against as if nothing
// had changed about what a datastore is.
func TestCountRefusesABareDocument(t *testing.T) {
	_, err := Count([]byte(`{"cards":[{"setCode":"A"}],"sealed":[]}`))
	if !errors.Is(err, emit.ErrNotEnvelope) {
		t.Errorf("Count(bare) error = %v, want ErrNotEnvelope", err)
	}
}

// TestCountReadsTheEnvelopeShape pins the reader every game but Riftbound
// uses: cards by set code and sealed by count, off the payload of the
// envelope a baseline and a build's own output both come in.
func TestCountReadsTheEnvelopeShape(t *testing.T) {
	got, err := Count(envelope(`{"cards":[{"setCode":"A"},{"setCode":"A"},{"setCode":"B"}],"sealed":[{},{}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cards != 3 || got.Sealed != 2 || got.BySet["A"] != 2 || got.BySet["B"] != 1 {
		t.Errorf("Count = %+v", got)
	}
}

// TestGuardOnlyMovesTheBaselineForward pins the high-water mark: a build
// inside the tolerance still publishes but is not fit to become the next
// baseline, and a build at least as large is.
func TestGuardOnlyMovesTheBaselineForward(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "previous.json")
	if err := os.WriteFile(baseline, envelope(`{"cards":[{"setCode":"A"},{"setCode":"A"}],"sealed":[{}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		desc string
		data string
		fit  bool
	}{
		{"the same size", `{"cards":[{"setCode":"A"},{"setCode":"A"}],"sealed":[{}]}`, true},
		{"larger", `{"cards":[{"setCode":"A"},{"setCode":"A"},{"setCode":"A"}],"sealed":[{}]}`, true},
		{"one sealed fewer", `{"cards":[{"setCode":"A"},{"setCode":"A"}],"sealed":[]}`, false},
	} {
		fitPath := filepath.Join(dir, "fit")
		if err := os.Remove(fitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		// A tolerance wide enough that nothing here is refused: fitness
		// is what is being read, not the refusal.
		err := Guard(envelope(test.data), Count, Options{Against: baseline, Tolerance: 1, FitPath: fitPath})
		if err != nil {
			t.Fatalf("%s: %v", test.desc, err)
		}
		if _, err := os.Stat(fitPath); (err == nil) != test.fit {
			t.Errorf("%s: fit file written = %v, want %v", test.desc, err == nil, test.fit)
		}
	}
}

// TestGuardRefusesWithTheCheckNamed pins that the reason a build is not
// published says which check refused it, the way the log used to.
func TestGuardRefusesWithTheCheckNamed(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "previous.json")
	if err := os.WriteFile(baseline, envelope(`{"cards":[{"setCode":"A"},{"setCode":"B"}],"sealed":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Guard(envelope(`{"cards":[{"setCode":"A"},{"setCode":"A"}],"sealed":[]}`), Count, Options{Against: baseline, Tolerance: 0.01})
	if err == nil || !strings.HasPrefix(err.Error(), "against: refusing to publish:") {
		t.Errorf("Guard = %v, want a refusal prefixed with the check", err)
	}
	if err := Guard([]byte(`{}`), Count, Options{}); err != nil {
		t.Errorf("Guard with nothing asked = %v, want nil", err)
	}
}

// envelope wraps a test datastore the way every builder publishes one.
func envelope(payload string) []byte {
	return []byte(`{"meta":{"date":"2026-09-24","version":"1"},"data":` + payload + `}`)
}
