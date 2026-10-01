package naming

import (
	"slices"
	"testing"
)

func TestSplitDropsTheNumberItWears(t *testing.T) {
	for _, test := range []struct {
		name, number, base string
		qualifiers         []string
	}{
		{"Resource (RP-045) (EVX07 Resource Set)", "RP-045", "Resource", []string{"EVX07 Resource Set"}},
		{"Delta Plus (Waverider Mode) - GD01-012", "GD01-012", "Delta Plus", []string{"Waverider Mode"}},
		{"Lamball ( )", "", "Lamball", nil},
		{"Striker Pack (GD01-012)", "GD01-012", "Striker Pack", nil},
		{"Striker Pack (gd01-012)", "GD01-012", "Striker Pack", nil},
	} {
		base, qualifiers := Split(test.name, test.number)
		if base != test.base || !slices.Equal(qualifiers, test.qualifiers) {
			t.Errorf("Split(%q, %q) = %q, %q; want %q, %q", test.name, test.number, base, qualifiers, test.base, test.qualifiers)
		}
	}
}

func TestRedundantReadsTheRarityAndTheNumber(t *testing.T) {
	for _, test := range []struct {
		qualifier, rarity, number string
		want                      bool
	}{
		{"C+", "C+", "GD01-001", true},
		{"Super", "Super Rare", "GD01-001", true},
		{"OSR", "Over Super Rare", "GD01-001", true},
		{"TSR", "Common", "EBP01-001TSR", true},
		{"MA Mode", "Rare", "GD01-012", false},
		{"", "Rare", "GD01-012", false},
	} {
		if got := Redundant(test.qualifier, test.rarity, test.number); got != test.want {
			t.Errorf("Redundant(%q, %q, %q) = %v, want %v", test.qualifier, test.rarity, test.number, got, test.want)
		}
	}
}

func TestProvenanceIsWholeWords(t *testing.T) {
	for _, test := range []struct {
		qualifier string
		want      bool
	}{
		{"EVX07 Resource Set", true},
		{"Store Championship", true},
		{"Judge Pack", true},
		{"MA Mode", false},
		{"Full Package", false},
		{"Sleeves", false},
	} {
		if got := Provenance(test.qualifier); got != test.want {
			t.Errorf("Provenance(%q) = %v, want %v", test.qualifier, got, test.want)
		}
	}
}
