package baseline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		{"sealed past the tolerance", counts(1000, 90, map[string]int{"A": 500, "B": 300, "C": 200}), "90 sealed products"},
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

// TestRegressionWithoutABaseline pins that a first build, measured against
// nothing, is not refused for anything.
func TestRegressionWithoutABaseline(t *testing.T) {
	if err := Regression(Counts{}, counts(1, 1, map[string]int{"A": 1}), 0.01); err != nil {
		t.Errorf("Regression against an empty baseline = %v, want nil", err)
	}
}

// TestCountReadsTheTopLevelShape pins the reader every game but Riftbound
// uses: cards by set code, sealed by count.
func TestCountReadsTheTopLevelShape(t *testing.T) {
	got, err := Count([]byte(`{"cards":[{"setCode":"A"},{"setCode":"A"},{"setCode":"B"}],"sealed":[{},{}]}`))
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
	if err := os.WriteFile(baseline, []byte(`{"cards":[{"setCode":"A"},{"setCode":"A"}],"sealed":[{}]}`), 0o600); err != nil {
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
		err := Guard([]byte(test.data), Count, Options{Against: baseline, Tolerance: 1, FitPath: fitPath})
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
	if err := os.WriteFile(baseline, []byte(`{"cards":[{"setCode":"A"},{"setCode":"B"}],"sealed":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Guard([]byte(`{"cards":[{"setCode":"A"},{"setCode":"A"}],"sealed":[]}`), Count, Options{Against: baseline, Tolerance: 0.01})
	if err == nil || !strings.HasPrefix(err.Error(), "against: refusing to publish:") {
		t.Errorf("Guard = %v, want a refusal prefixed with the check", err)
	}
	if err := Guard([]byte(`{}`), Count, Options{}); err != nil {
		t.Errorf("Guard with nothing asked = %v, want nil", err)
	}
}
