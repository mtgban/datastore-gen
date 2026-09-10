// Package emit spells the parts of a datastore every builder writes the same
// way: the finish suffix an id carries, which printing is the plain one, the
// order a product's entries come out in, the slug a promo type is, the quotes
// a name may carry and the image link a card publishes - and reads the two
// things every builder reads the same way, an upstream list and a list of
// strings off an entry.
//
// Each of these was a function copied into every builder, byte for byte or
// a spelling apart - nine of them, in up to eight places. The builders stay
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
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

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

// upstreamClient bounds a fetch from an upstream list: a hung upstream hangs
// the publish otherwise, since http.Get waits forever.
var upstreamClient = &http.Client{Timeout: 3 * time.Minute}

// userAgent names this build in an upstream's logs, so the mirror can tell
// it from a browser.
const userAgent = "datastore-gen/1.0 (+https://github.com/mtgban/datastore-gen)"

// Fetch reads a local path, or an http(s) location when one is given, so a
// build can be pinned to a file and the default can be the live URL. Six
// builders carried this in three spellings: four read the location with
// http.Get, one named itself and bounded the wait, one pretended to be a
// browser for a page that serves the same bytes either way.
func Fetch(location string) ([]byte, error) {
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") {
		return os.ReadFile(location)
	}
	req, err := http.NewRequest(http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", location, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// StringsOf reads a list of strings back off an entry, which holds them as
// []string before the document is encoded and []any after. An empty string
// in the list is nothing and is left out.
func StringsOf(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, raw := range list {
			if s, ok := raw.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
