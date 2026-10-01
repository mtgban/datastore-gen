package handtable

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

// TestReportNamesTheRowsNothingUsed pins what a build learns about its hand
// tables: the rows no lookup hit, in order, and nothing for a table whose
// every row was used or for a lookup that missed.
func TestReportNamesTheRowsNothingUsed(t *testing.T) {
	mu.Lock()
	registry = nil
	mu.Unlock()
	spellings := New("spellings", map[string]string{"a": "A", "b": "B", "c": "C"})
	subjects := New("subjects", map[string]bool{"x": true})
	shelves := NewList("shelves", "first", "second", "third")

	if v, ok := spellings.Get("b"); !ok || v != "B" {
		t.Errorf("Get(b) = %q, %v", v, ok)
	}
	if spellings.Has("z") {
		t.Error("Has(z) on a table without it")
	}
	subjects.Has("x")
	shelves.Use("second")

	var out bytes.Buffer
	log.SetOutput(&out)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	}()
	Report()
	got := out.String()
	for _, want := range []string{
		"hand table spellings: 2 of 3 rows used by nothing: a, c\n",
		"hand table shelves: 2 of 3 rows used by nothing: first, third\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "subjects") {
		t.Errorf("report names a table every row of which was used:\n%s", got)
	}
}
