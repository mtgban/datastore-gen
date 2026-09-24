package main

import (
	"fmt"
	"slices"
	"testing"
)

// row is a gallery card row as the build holds it: the id, the set, and the
// product that priced it, 0 for none.
func row(id, set string, product int) map[string]any {
	r := map[string]any{
		"id":  id,
		"set": map[string]any{"value": map[string]any{"id": set}},
	}
	if product != 0 {
		r["tcgplayerProductId"] = product
	}
	return r
}

func idsOf(items []any) []string {
	var out []string
	for _, raw := range items {
		out = append(out, fmt.Sprint(raw.(map[string]any)["id"]))
	}
	return out
}

// TestRespellSharedIDs pins the rule on the two shapes the gallery can share
// an id in. The first is the one that stopped the builds of 2026-09-20 to
// 09-22: the Signature #192 wore the Overnumbered row's id and number, so one
// row was priced and the other not. Each row stays, under an id of its own.
func TestRespellSharedIDs(t *testing.T) {
	for _, test := range []struct {
		desc string
		in   []any
		want []string
	}{
		{
			desc: "the priced row keeps the id, so its product keeps its uuid",
			in:   []any{row("ven-192-166", "VEN", 0), row("ven-192-166", "VEN", 706014)},
			want: []string{"ven-192-166-2", "ven-192-166"},
		},
		{
			desc: "a second priced row is spelled from its own product",
			in:   []any{row("ven-192-166", "VEN", 706014), row("ven-192-166", "VEN", 709304)},
			want: []string{"ven-192-166", "ven-709304"},
		},
		{
			desc: "with none priced, the first row keeps it",
			in:   []any{row("ogn-001-298", "OGN", 0), row("ogn-001-298", "OGN", 0), row("ogn-001-298", "OGN", 0)},
			want: []string{"ogn-001-298", "ogn-001-298-2", "ogn-001-298-3"},
		},
		{
			desc: "a spelling already taken moves on rather than colliding",
			in:   []any{row("ven-192-166", "VEN", 0), row("ven-192-166", "VEN", 0), row("ven-192-166-2", "VEN", 0)},
			want: []string{"ven-192-166", "ven-192-166-3", "ven-192-166-2"},
		},
		{
			desc: "rows that share nothing are left alone",
			in:   []any{row("ven-192-166", "VEN", 706014), row("ven-192-star-166", "VEN", 709304)},
			want: []string{"ven-192-166", "ven-192-star-166"},
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			before := idsOf(test.in)
			respelled := respellSharedIDs(test.in)
			got := idsOf(test.in)
			if !slices.Equal(got, test.want) {
				t.Errorf("ids = %v, want %v", got, test.want)
			}
			seen := map[string]bool{}
			for _, id := range got {
				if seen[id] {
					t.Errorf("id %s is still shared", id)
				}
				seen[id] = true
			}
			var changed int
			for i := range got {
				if got[i] != before[i] {
					changed++
				}
			}
			if len(respelled) != changed {
				t.Errorf("reported %d respellings %v, changed %d ids", len(respelled), respelled, changed)
			}
		})
	}
}
