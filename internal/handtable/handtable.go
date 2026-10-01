// Package handtable holds the tables a builder carries by hand - a subject,
// a respelling, a name the catalog got wrong - and says which of their rows
// a build never used. A row nothing uses is a fact a source now carries or a
// product that is gone, and a table that cannot say so only ever grows.
package handtable

import (
	"cmp"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
)

// reporter is a table Report can ask about its unused rows.
type reporter interface {
	unused() (name string, rows []string, size int)
}

var (
	mu       sync.Mutex
	registry []reporter
)

func register(r reporter) {
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, r)
}

// Table is a map carried by hand.
type Table[K cmp.Ordered, V any] struct {
	name string
	rows map[K]V
	used map[K]bool
}

// New carries rows by hand under a name the report gives them.
func New[K cmp.Ordered, V any](name string, rows map[K]V) *Table[K, V] {
	t := &Table[K, V]{name: name, rows: rows, used: map[K]bool{}}
	register(t)
	return t
}

// Get looks a key up, and marks its row used when there is one.
func (t *Table[K, V]) Get(key K) (V, bool) {
	value, found := t.rows[key]
	if found {
		t.Use(key)
	}
	return value, found
}

// Has reports whether the table holds a key, marking its row used.
func (t *Table[K, V]) Has(key K) bool {
	_, found := t.Get(key)
	return found
}

// Rows is the table itself, for a builder that scans every row; it marks
// the rows it matches with Use.
func (t *Table[K, V]) Rows() map[K]V {
	return t.rows
}

// Use marks a row used.
func (t *Table[K, V]) Use(key K) {
	mu.Lock()
	defer mu.Unlock()
	t.used[key] = true
}

func (t *Table[K, V]) unused() (string, []string, int) {
	mu.Lock()
	defer mu.Unlock()
	var keys []K
	for key := range t.rows {
		if !t.used[key] {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	rows := make([]string, len(keys))
	for i, key := range keys {
		rows[i] = fmt.Sprint(key)
	}
	return t.name, rows, len(t.rows)
}

// List is an ordered list carried by hand, scanned in its order.
type List struct {
	name  string
	items []string
	used  map[string]bool
}

// NewList carries items by hand under a name the report gives them.
func NewList(name string, items ...string) *List {
	l := &List{name: name, items: items, used: map[string]bool{}}
	register(l)
	return l
}

// Items is the list in its order; a scan marks the item it matches with Use.
func (l *List) Items() []string {
	return l.items
}

// Use marks an item used.
func (l *List) Use(item string) {
	mu.Lock()
	defer mu.Unlock()
	l.used[item] = true
}

func (l *List) unused() (string, []string, int) {
	mu.Lock()
	defer mu.Unlock()
	var rows []string
	for _, item := range l.items {
		if !l.used[item] {
			rows = append(rows, item)
		}
	}
	return l.name, rows, len(l.items)
}

// Report logs, for every table carried by hand, the rows this build used
// nothing of.
func Report() {
	mu.Lock()
	tables := append([]reporter(nil), registry...)
	mu.Unlock()
	for _, table := range tables {
		name, rows, size := table.unused()
		if len(rows) == 0 {
			continue
		}
		log.Printf("hand table %s: %d of %d rows used by nothing: %s",
			name, len(rows), size, strings.Join(rows, ", "))
	}
}
