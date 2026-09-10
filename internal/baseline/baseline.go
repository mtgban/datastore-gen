// Package baseline compares a build against the datastore it is about to
// replace and refuses one that lost a meaningful share of it.
//
// Every builder used to carry its own copy of this, and the eight copies
// were identical down to the comments - which is the one kind of duplication
// that buys nothing. The builders stay standalone in the sense that matters:
// no dependency on go-mtgban, no external module beyond the catalog reader.
// A package inside this module is neither.
//
// The minimum card count this used to be checked against was a number
// invented once and never revisited, far below what a datastore actually
// holds, so a build could lose a third of itself and still publish. The
// previous datastore is the number that keeps itself up to date.
package baseline

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

// Counts is what a datastore holds: the two totals, and the card count per
// set. It is read off an encoded datastore - this build's own, or the one it
// is about to replace - so both sides are counted the same way by the same
// code.
type Counts struct {
	Cards, Sealed int
	BySet         map[string]int
}

// Count reads the counts off a datastore that keeps its cards and sealed
// products at the top level, which is every game but Riftbound. A game
// whose datastore is shaped otherwise reads its own counts and hands them
// to Guard through a Reader of its own.
func Count(data []byte) (Counts, error) {
	var doc struct {
		Cards []struct {
			SetCode string `json:"setCode"`
		} `json:"cards"`
		Sealed []json.RawMessage `json:"sealed"`
	}
	out := Counts{BySet: map[string]int{}}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, err
	}
	out.Cards = len(doc.Cards)
	out.Sealed = len(doc.Sealed)
	for _, card := range doc.Cards {
		out.BySet[card.SetCode]++
	}
	return out, nil
}

// Reader reads a datastore's counts off its encoded form.
type Reader func(data []byte) (Counts, error)

// Regression compares this build against the one it is about to replace.
//
// Only shrinkage is suspicious - these datastores grow every week - and
// only three shapes of it are refused: a total that fell by more than the
// tolerance, a set that holds no card at all any more, and a set that lost
// more than half of what it held. The last two are what a whole-file count
// cannot see: one set folding onto another moves the total by a fraction
// of a percent while emptying a set completely. Every other per-set drop is
// logged rather than refused, because a product delisted here and there is
// ordinary and a build that cried wolf would be turned off.
func Regression(previous, current Counts, tolerance float64) error {
	if previous.Cards == 0 {
		return nil
	}
	lost := func(was, now int) bool {
		return now < was && float64(was-now)/float64(was) > tolerance
	}
	if lost(previous.Cards, current.Cards) {
		return fmt.Errorf("%d cards, down from %d, more than the %.1f%% a build may lose",
			current.Cards, previous.Cards, tolerance*100)
	}
	if lost(previous.Sealed, current.Sealed) {
		return fmt.Errorf("%d sealed products, down from %d, more than the %.1f%% a build may lose",
			current.Sealed, previous.Sealed, tolerance*100)
	}
	var vanished, collapsed, shrank []string
	for code, was := range previous.BySet {
		now := current.BySet[code]
		switch {
		case now == 0:
			vanished = append(vanished, code)
		case now*2 < was:
			collapsed = append(collapsed, fmt.Sprintf("%s %d->%d", code, was, now))
		case now < was:
			shrank = append(shrank, fmt.Sprintf("%s %d->%d", code, was, now))
		}
	}
	sort.Strings(vanished)
	sort.Strings(collapsed)
	sort.Strings(shrank)
	for _, s := range shrank {
		log.Printf("against: set %s", s)
	}
	if len(vanished) > 0 {
		return fmt.Errorf("%d sets hold no card any more: %s",
			len(vanished), strings.Join(vanished, " "))
	}
	if len(collapsed) > 0 {
		return fmt.Errorf("%d sets lost more than half of what they held: %s",
			len(collapsed), strings.Join(collapsed, " "))
	}
	return nil
}

// Options is what the publish hands a build about its baseline.
type Options struct {
	// Against is the baseline datastore to compare against, or "" for none.
	Against string
	// Tolerance is the share of its cards or sealed products a build may
	// lose before it is refused.
	Tolerance float64
	// FitPath, when set, is written when the build is fit to become the
	// baseline the next build compares against.
	FitPath string
	// Unit names a card entry in the log - "cards" for most games,
	// "printings" where the datastore calls them that. Empty means cards.
	Unit string
}

// Guard measures the encoded build against the baseline, refuses it when
// Regression says so, and says whether it is fit to become the next one.
//
// The baseline only ever moves forward. A build smaller than it -
// legitimately, within the tolerance - must not become the thing the next
// build is measured against, or a run of tolerated drops ratchets it down
// one step at a time and the whole loss is never large enough for any
// single run to see. Measuring from the high-water mark instead means the
// drift has to stay under the tolerance in total, not per night.
//
// A returned error is the reason the build must not be published, already
// prefixed with the check that refused it.
func Guard(data []byte, read Reader, opts Options) error {
	if opts.Against == "" && opts.FitPath == "" {
		return nil
	}
	unit := opts.Unit
	if unit == "" {
		unit = "cards"
	}
	current, err := read(data)
	if err != nil {
		return fmt.Errorf("against: %w", err)
	}
	fit := true
	if opts.Against != "" {
		previousData, err := os.ReadFile(opts.Against)
		if err != nil {
			return fmt.Errorf("against: %w", err)
		}
		previous, err := read(previousData)
		if err != nil {
			return fmt.Errorf("against: %w", err)
		}
		log.Printf("against %s: %d %s (was %d), %d sealed (was %d), %d sets (was %d)",
			opts.Against, current.Cards, unit, previous.Cards, current.Sealed, previous.Sealed,
			len(current.BySet), len(previous.BySet))
		if err := Regression(previous, current, opts.Tolerance); err != nil {
			return fmt.Errorf("against: refusing to publish: %w", err)
		}
		fit = current.Cards >= previous.Cards && current.Sealed >= previous.Sealed
	}
	if opts.FitPath == "" {
		return nil
	}
	if !fit {
		log.Print("baseline: unchanged, this build holds less than it does")
		return nil
	}
	note := fmt.Sprintf("cards=%d sealed=%d\n", current.Cards, current.Sealed)
	if err := os.WriteFile(opts.FitPath, []byte(note), 0o644); err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	log.Print("baseline: this build becomes the one the next is measured against")
	return nil
}
