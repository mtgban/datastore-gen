// Package emit spells the parts of a datastore every builder writes the same
// way: the finish suffix an id carries, which printing is the plain one, the
// order a product's entries come out in, the slug a promo type is, the quotes
// a name may carry and the image link a card publishes.
//
// Each of these was a function copied into every builder, byte for byte,
// comment and all - seven of them, in up to eight places. The builders stay
// standalone in the sense that matters: no dependency on go-mtgban, no
// external module beyond the catalog reader. A package inside this module
// is neither, and a rule stated once cannot drift between games.
//
// internal/vocabulary keeps a Slug of its own on purpose. It is the check
// that a published datastore reads the way the builders agree it should,
// and a check that spelled its tokens with the builders' own function would
// pass a builder whose spelling had gone wrong.
package emit

import (
	"slices"
	"sort"
	"strings"

	"github.com/mtgban/go-tcgplayer"
)

// FinishSlug spells a printing name the way an id carries it.
func FinishSlug(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// FinishSuffix is the id suffix a printing's entries carry: nothing for the
// plain printing, and the printing's own name for every other. TCGplayer
// calls a plain printing "Normal" in every category, which is the one
// convention here rather than a list of this category's printings - those
// are the catalog's to name, to add to and to rename, and every one of them
// reaches an id without a release.
func FinishSuffix(name string) string {
	if slug := FinishSlug(name); slug != "" && slug != "normal" {
		return "_" + slug
	}
	return ""
}

// PlainPrinting is the catalog's name for the printing a bare id belongs to,
// or "" where the category has none - Yu-Gi-Oh prices its cards by print run
// and sells no printing it calls plain.
func PlainPrinting(c *tcgplayer.CatalogDump) string {
	for _, printing := range c.Printings {
		if FinishSlug(printing.Name) == "normal" {
			return printing.Name
		}
	}
	return ""
}

// OrderedFinishes fixes the order a product's entries are emitted in: the
// order TCGplayer displays the category's printings in, which is the
// catalog's to decide. Two printings can share a displayOrder, so the name
// settles a tie and unchanged data keeps producing byte-identical output.
func OrderedFinishes(names []string, rank map[string]int) []string {
	out := slices.Clone(names)
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank[out[i]], rank[out[j]]; ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// PromoSlug spells a label the way every promo type here is spelled: lower
// case, letters and digits and nothing else. It is a token for a consumer
// to interpret and a query to carry, not words for a reader - what a
// promotion is shown as is the loader's to decide, and the words are in the
// variant beside it, which every build already writes. It is the same
// spelling a finish takes in an id, kept under the name of what it spells.
func PromoSlug(label string) string {
	return FinishSlug(label)
}

// typographic is the quotes a catalog spells with and no consumer queries
// with.
var typographic = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'", "\u201c", `"`, "\u201d", `"`)

// PlainQuotes rewrites those quotes wherever the document carries them.
// The catalogs are not consistent about it: TCGplayer sells "Rocket's
// Hitmonchan" with a curly apostrophe beside hundreds of names holding a
// plain one, files two Yu-Gi-Oh rarities as "Ultra Pharaoh's Rare" while
// every other name uses ASCII, and spells one One Piece card
// Eustass"Captain"Kid on the card and Eustass"Captain"Kid on the box it
// comes in. A query carries one spelling, so the card filed under the
// other cannot be found, and the two rarities cannot be asked for at all.
//
// The whole document is walked rather than the fields known to carry
// them, because the field that starts carrying them tomorrow would
// otherwise be missed, and it runs before the output is encoded so the
// check that re-reads it sees exactly what will be published.
func PlainQuotes(v any) any {
	switch t := v.(type) {
	case string:
		return typographic.Replace(t)
	case map[string]any:
		for k, e := range t {
			t[k] = PlainQuotes(e)
		}
	case []any:
		for i, e := range t {
			t[i] = PlainQuotes(e)
		}
	}
	return v
}

// ImageURL upgrades a catalog image link to the 400-wide rendition; the
// dump links the smallest one there is.
func ImageURL(url string) string {
	return strings.Replace(url, "_200w.", "_400w.", 1)
}
