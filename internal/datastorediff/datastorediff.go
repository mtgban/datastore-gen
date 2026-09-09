// Package datastorediff says what one build of a datastore did to the last
// one, in the terms a reader of the history would ask about.
//
// It exists so that the repository can be tagged by what a commit actually
// published rather than by what its message says it did. Roughly half the
// commits here leave the built datastore byte-identical - a comment, a log
// line, a refactor that lands on the same output - and a tag on every one
// of those says nothing.
//
// The comparison is only meaningful when both sides were built from one
// catalog. Two builds a day apart differ by every card TCGplayer added in
// between, which is not what any of this is trying to measure; the caller
// holds the inputs still and this reports what is left, which is the code's
// doing.
//
// What it distinguishes matters as much as what it counts. A commit that
// starts publishing a field is a different event from one that reworded
// values in a field already published, and both read as "entries changed"
// to anything comparing whole records. Fields are diffed by name, so
// "language on 9 entries" and "promoTypes off 8" is what a commit
// publishing a language reads as - including, as there, the field moving
// from one name to another.
package datastorediff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Change is what one build did to the one before it. The zero value is no
// change at all, which is what most commits produce.
type Change struct {
	// IDsAdded and IDsRemoved count entries by their uuid: cards this
	// build publishes that the last did not, and the other way round.
	IDsAdded, IDsRemoved int

	// FieldsAdded and FieldsRemoved count, per field name, the entries
	// that gained or lost it. A field the datastore never published
	// before shows up here on every entry that carries it.
	FieldsAdded, FieldsRemoved map[string]int

	// ValuesChanged counts, per field name, the entries whose value for a
	// field both builds publish is not the same value.
	ValuesChanged map[string]int

	// Sets and Sealed are counted whole, since a set folding into another
	// or a product leaving the catalog is a fact about the shelf rather
	// than about any one entry.
	SetsBefore, SetsAfter, SetsReworded int
	SealedBefore, SealedAfter           int
	SealedReworded                      bool

	// Leaves is the fallback for a datastore that publishes no top-level
	// cards key at all. Riftbound's output is the upstream payload with
	// the catalog merged into it, so the entries sit deep inside a
	// structure that is not a list of cards; there is nothing to key on,
	// and the whole document is compared leaf by leaf instead.
	LeavesAdded, LeavesRemoved, LeavesChanged int
	ByLeaves                                  bool
}

// Empty reports whether the two builds published the same datastore.
func (c Change) Empty() bool {
	return c.IDsAdded == 0 && c.IDsRemoved == 0 &&
		len(c.FieldsAdded) == 0 && len(c.FieldsRemoved) == 0 && len(c.ValuesChanged) == 0 &&
		c.SetsBefore == c.SetsAfter && c.SetsReworded == 0 &&
		c.SealedBefore == c.SealedAfter && !c.SealedReworded &&
		c.LeavesAdded == 0 && c.LeavesRemoved == 0 && c.LeavesChanged == 0
}

// String is the one line a tag carries: what changed, most structural
// first, so a reader sees "ids +212" before "3 entries reworded".
func (c Change) String() string {
	if c.Empty() {
		return "no change"
	}
	var parts []string
	if c.ByLeaves {
		if c.LeavesAdded != 0 || c.LeavesRemoved != 0 {
			parts = append(parts, fmt.Sprintf("fields +%d/-%d", c.LeavesAdded, c.LeavesRemoved))
		}
		if c.LeavesChanged != 0 {
			parts = append(parts, fmt.Sprintf("%d values changed", c.LeavesChanged))
		}
		return strings.Join(parts, "; ")
	}
	if c.IDsAdded != 0 || c.IDsRemoved != 0 {
		parts = append(parts, fmt.Sprintf("ids +%d/-%d", c.IDsAdded, c.IDsRemoved))
	}
	parts = append(parts, byField("published", c.FieldsAdded)...)
	parts = append(parts, byField("dropped", c.FieldsRemoved)...)
	parts = append(parts, byField("reworded", c.ValuesChanged)...)
	if c.SetsBefore != c.SetsAfter {
		parts = append(parts, fmt.Sprintf("sets %d->%d", c.SetsBefore, c.SetsAfter))
	} else if c.SetsReworded != 0 {
		parts = append(parts, fmt.Sprintf("%d sets reworded", c.SetsReworded))
	}
	if c.SealedBefore != c.SealedAfter {
		parts = append(parts, fmt.Sprintf("sealed %d->%d", c.SealedBefore, c.SealedAfter))
	} else if c.SealedReworded {
		parts = append(parts, "sealed reworded")
	}
	return strings.Join(parts, "; ")
}

// byField renders a per-field count as "published language on 9", naming
// the fields in a fixed order so one build's summary can be compared with
// another's.
func byField(verb string, counts map[string]int) []string {
	if len(counts) == 0 {
		return nil
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s %s on %d", verb, name, counts[name]))
	}
	return out
}

// Compare says what the second datastore did to the first. Both are the
// encoded documents a builder writes, and both must have been built from
// one catalog for the answer to be about the code.
func Compare(before, after []byte) (Change, error) {
	var a, b map[string]any
	if err := json.Unmarshal(before, &a); err != nil {
		return Change{}, fmt.Errorf("before: %w", err)
	}
	if err := json.Unmarshal(after, &b); err != nil {
		return Change{}, fmt.Errorf("after: %w", err)
	}

	ca, aKeyed := entries(a)
	cb, bKeyed := entries(b)
	if !aKeyed || !bKeyed {
		// No cards key to hold entries apart: compare the documents leaf
		// by leaf instead, which says how much moved without pretending
		// to know which card moved.
		return byLeaves(a, b), nil
	}

	change := Change{
		FieldsAdded:   map[string]int{},
		FieldsRemoved: map[string]int{},
		ValuesChanged: map[string]int{},
	}
	for id := range cb {
		if _, held := ca[id]; !held {
			change.IDsAdded++
		}
	}
	for id := range ca {
		if _, held := cb[id]; !held {
			change.IDsRemoved++
		}
	}
	// Only entries both builds publish can say anything about a field:
	// one that arrived with its entry is the entry's news, not the
	// field's, and counting it here would report every new card as every
	// field being published again.
	for id, entryA := range ca {
		entryB, held := cb[id]
		if !held {
			continue
		}
		fa, aOK := entryA.(map[string]any)
		fb, bOK := entryB.(map[string]any)
		if !aOK || !bOK {
			continue
		}
		for name := range fb {
			if _, had := fa[name]; !had {
				change.FieldsAdded[name]++
			}
		}
		for name, was := range fa {
			now, still := fb[name]
			if !still {
				change.FieldsRemoved[name]++
				continue
			}
			if !sameJSON(was, now) {
				change.ValuesChanged[name]++
			}
		}
	}

	sa, _ := a["sets"].(map[string]any)
	sb, _ := b["sets"].(map[string]any)
	change.SetsBefore, change.SetsAfter = len(sa), len(sb)
	if change.SetsBefore == change.SetsAfter {
		for code, was := range sa {
			if now, held := sb[code]; !held || !sameJSON(was, now) {
				change.SetsReworded++
			}
		}
	}

	la, _ := a["sealed"].([]any)
	lb, _ := b["sealed"].([]any)
	change.SealedBefore, change.SealedAfter = len(la), len(lb)
	if change.SealedBefore == change.SealedAfter {
		change.SealedReworded = !sameJSON(la, lb)
	}
	return change, nil
}

// entries pulls the cards out of a document by uuid, reporting whether the
// document holds them somewhere this can key on at all. A builder emits
// them as a list; a document that publishes them under some other shape,
// or not at the top level, is compared by leaves instead.
func entries(doc map[string]any) (map[string]any, bool) {
	held, found := doc["cards"]
	if !found {
		// Riftbound's upstream is the card gallery Riot serves its own
		// site, and the builder publishes that document with the cards
		// where they already were. internal/vocabulary walks the same
		// path for the same reason; a reader that stops at the top level
		// sees none and calls the whole game unchanged.
		page, _ := doc["pageProps"].(map[string]any)
		inner, _ := page["page"].(map[string]any)
		if blades, ok := inner["blades"].([]any); ok {
			for _, blade := range blades {
				fields, ok := blade.(map[string]any)
				if !ok {
					continue
				}
				gallery, _ := fields["cards"].(map[string]any)
				if items, ok := gallery["items"].([]any); ok && len(items) > 0 {
					held = items
					break
				}
			}
		}
	}
	switch cards := held.(type) {
	case []any:
		out := make(map[string]any, len(cards))
		for i, entry := range cards {
			id := ""
			if fields, ok := entry.(map[string]any); ok {
				id, _ = fields["id"].(string)
				if id == "" {
					// A gallery card is named by the code printed on it.
					id, _ = fields["publicCode"].(string)
				}
			}
			if id == "" {
				// An entry with no uuid cannot be told from another; key
				// it on its position so the count still moves when one
				// arrives or leaves.
				id = fmt.Sprintf("#%d", i)
			}
			out[id] = entry
		}
		return out, true
	case map[string]any:
		return cards, true
	}
	return nil, false
}

// byLeaves compares two documents value by value, down every path. It says
// how much of the document moved without naming cards, which is all that
// can honestly be said about a shape with no entries to key on.
func byLeaves(a, b map[string]any) Change {
	la, lb := map[string]any{}, map[string]any{}
	walk("", a, la)
	walk("", b, lb)
	change := Change{ByLeaves: true}
	for path := range lb {
		if _, held := la[path]; !held {
			change.LeavesAdded++
		}
	}
	for path, was := range la {
		now, held := lb[path]
		if !held {
			change.LeavesRemoved++
			continue
		}
		if !sameJSON(was, now) {
			change.LeavesChanged++
		}
	}
	return change
}

func walk(path string, value any, into map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			walk(path+"."+key, child, into)
		}
	case []any:
		for i, child := range v {
			walk(fmt.Sprintf("%s[%d]", path, i), child, into)
		}
	default:
		into[path] = v
	}
}

// sameJSON reports whether two decoded values are the same value. Decoded
// JSON holds maps and slices, which are not comparable with ==, and
// re-encoding both is the shortest thing that is correct for every shape a
// datastore carries.
func sameJSON(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ea, err := json.Marshal(a)
	if err != nil {
		return false
	}
	eb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ea) == string(eb)
}
