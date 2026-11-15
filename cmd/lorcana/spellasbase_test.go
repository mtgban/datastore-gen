package main

import "testing"

// TestSpellAsBase pins that a reprint written apart from its base by case
// alone takes the base's spelling, and that a reprint under another name, or
// one whose base is not carried, keeps its own.
func TestSpellAsBase(t *testing.T) {
	base := map[string]any{"id": float64(72), "name": "Cruella De Vil", "version": "Miserable as Usual", "fullName": "Cruella De Vil - Miserable as Usual"}
	reprint := map[string]any{"id": float64(662), "baseId": float64(72), "name": "Cruella De Vil", "version": "Miserable As Usual", "fullName": "Cruella De Vil - Miserable As Usual"}
	song := map[string]any{"id": float64(163), "name": "Let It Go", "fullName": "Let It Go"}
	songReprint := map[string]any{"id": float64(2460), "baseId": float64(163), "name": "Let it Go", "fullName": "Let it Go"}
	renamed := map[string]any{"id": float64(900), "baseId": float64(163), "name": "Vaiana", "fullName": "Vaiana"}
	orphan := map[string]any{"id": float64(901), "baseId": float64(5), "name": "Let it Go", "fullName": "Let it Go"}

	respelled := spellAsBase([]any{base, reprint, song, songReprint, renamed, orphan})

	if len(respelled) != 2 {
		t.Errorf("respelled %d cards, want 2: %v", len(respelled), respelled)
	}
	if reprint["version"] != "Miserable as Usual" || reprint["fullName"] != base["fullName"] {
		t.Errorf("reprint = %v %v, want the base's spelling", reprint["version"], reprint["fullName"])
	}
	if songReprint["name"] != "Let It Go" || songReprint["fullName"] != "Let It Go" {
		t.Errorf("song reprint = %v / %v, want Let It Go", songReprint["name"], songReprint["fullName"])
	}
	if _, ok := songReprint["version"]; ok {
		t.Error("song reprint gained a version its base does not have")
	}
	if renamed["fullName"] != "Vaiana" || orphan["fullName"] != "Let it Go" {
		t.Errorf("renamed = %v, orphan = %v, want both untouched", renamed["fullName"], orphan["fullName"])
	}
}
