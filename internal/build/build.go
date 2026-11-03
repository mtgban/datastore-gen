// Package build is what every builder's main does the same way: the flags
// every build takes, the catalog read, the split of the catalog's products
// into singles, sealed and unpriced, the coverage contract, the set and
// sealed entries, and the tail that encodes the document, fits its
// vocabulary, re-reads it, recounts it, guards it against the baseline and
// writes it. What is left in a builder's main is its game's join.
//
// The pieces are called by the builder, in its own order, rather than run as
// one pipeline around it: the builders log between them, and the publish and
// the measure workflow diff those logs.
package build

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/mtgban/datastore-gen/internal/baseline"
	"github.com/mtgban/datastore-gen/internal/emit"
	"github.com/mtgban/datastore-gen/internal/handtable"
	"github.com/mtgban/datastore-gen/internal/validate"
	"github.com/mtgban/datastore-gen/internal/vocabulary"

	"github.com/mtgban/go-tcgplayer"
)

// Game is what a builder says about its game to the steps every build takes
// alike.
type Game struct {
	// Category is the TCGplayer category the catalog dump must be for.
	Category int
	// Rules are the game's checks for the re-read of the output; Rules.Game
	// is the document's game field.
	Rules validate.Rules
	// Carries says what the catalog dump carries, in the refusal of a build
	// given none. Empty means "the printings and the ids".
	Carries string
	// Count reads a built document for the baseline guard. Nil means
	// baseline.Count, for a document of flat card entries.
	Count baseline.Reader
	// Unit names a card entry in the guard's log; empty means cards.
	Unit string
}

// Build is one run of a builder: its game and the flags every build takes.
type Build struct {
	Game

	// Output is the file written, or "" for stdout.
	Output string
	// CatalogPath is the tcgdumper catalog dump.
	CatalogPath string
	// UpstreamCache is the directory of last good upstream responses.
	UpstreamCache string
	// Against is the baseline datastore to compare against.
	Against string
	// Tolerance is the share of its cards or sealed products a build may lose.
	Tolerance float64
	// BaselineFit is written when the build is fit to become the baseline.
	BaselineFit string
}

// Start adds the flags every build takes to the ones the builder defined,
// parses the command line, and refuses a build given no catalog.
func Start(game Game) *Build {
	b, err := start(game, flag.CommandLine, os.Args[1:])
	if err != nil {
		log.Fatalln(err)
	}
	return b
}

func start(game Game, fs *flag.FlagSet, args []string) (*Build, error) {
	b := &Build{Game: game}
	fs.StringVar(&b.Output, "o", "", "output file (default stdout)")
	fs.StringVar(&b.CatalogPath, "tcg-catalog", "", fmt.Sprintf("tcgdumper catalog dump for category %d (required)", game.Category))
	fs.StringVar(&b.UpstreamCache, "upstream-cache", "", "directory holding the last good upstream responses, refreshed on a live fetch and read back when an upstream is unreachable")
	fs.StringVar(&b.Against, "against", "", "baseline datastore to compare against; refuses a build that lost a large share of it")
	fs.Float64Var(&b.Tolerance, "against-tolerance", 0.01, "the share of its cards or sealed products a build may lose")
	fs.StringVar(&b.BaselineFit, "baseline-fit", "", "write this file when the build is fit to become the baseline the next build compares against")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if b.CatalogPath == "" {
		return nil, fmt.Errorf("-tcg-catalog is required: the dump carries %s", cmp.Or(game.Carries, "the printings and the ids"))
	}
	return b, nil
}

// ReadCatalog reads the catalog dump, refusing one for another category.
func (b *Build) ReadCatalog() tcgplayer.CatalogDump {
	catalog, err := emit.ReadCatalog(b.CatalogPath, b.Category)
	if err != nil {
		log.Fatalln("tcg catalog:", err)
	}
	return catalog
}

// LogCatalogf says how large the catalog is, followed by format spelled
// with args: what the builder read beside it.
func LogCatalogf(catalog *tcgplayer.CatalogDump, format string, args ...any) {
	log.Printf("catalog: %d groups, %d products%s",
		len(catalog.Groups), len(catalog.Products), fmt.Sprintf(format, args...))
}

// GroupsByID indexes the catalog's groups.
func GroupsByID(groups []tcgplayer.Group) map[int]tcgplayer.Group {
	out := map[int]tcgplayer.Group{}
	for _, group := range groups {
		out[group.GroupID] = group
	}
	return out
}

// Split is the catalog's products as a build carries them: every product
// typed as a single and sold in a printing becomes card entries, every
// product typed as anything else is sealed, and a single nothing is sold
// for yet waits until something is.
type Split struct {
	// Products is how many products the catalog holds.
	Products int
	// Cards are the singles sold in at least one printing, in catalog order.
	Cards []tcgplayer.Product
	// Sealed are the products typed as anything but a single.
	Sealed []tcgplayer.Product
	// Unpriced names each single sold in no printing yet, as "name" (id).
	Unpriced []string
}

// SplitProducts splits the catalog's products by the single types and the
// printings each product is sold in.
func SplitProducts(catalog *tcgplayer.CatalogDump, singles []string, printings map[int][]string) Split {
	split := Split{Products: len(catalog.Products)}
	for _, product := range catalog.Products {
		if !slices.Contains(singles, product.ProductType) {
			split.Sealed = append(split.Sealed, product)
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			// Nothing is sold for it yet: no printing to carry until there is.
			split.Unpriced = append(split.Unpriced, fmt.Sprintf("%q (%d)", product.Name, product.ProductID))
			continue
		}
		split.Cards = append(split.Cards, product)
	}
	return split
}

// LogUnpriced names the singles waiting for a printing, when there are any;
// sku is what TCGplayer sells none of for them.
func (s Split) LogUnpriced(sku string) {
	if len(s.Unpriced) > 0 {
		log.Printf("unpriced: %d card products TCGplayer sells no %s for yet, carried once it does: %s",
			len(s.Unpriced), sku, strings.Join(s.Unpriced, ", "))
	}
}

// RequireCards refuses a catalog with no product typed as a single, which is
// a dump from before the product type was recorded: sealed by exclusion
// would file the whole catalog as sealed.
func (s Split) RequireCards() {
	if len(s.Cards) == 0 {
		log.Fatalln("tcg catalog: no products typed as singles; re-dump with a tcgdumper that records the product type")
	}
}

// Coverage is the coverage contract: every product the catalog types as a
// single, with the printings it is sold in. validate reads it back off the
// encoded output, so a product no rule carried fails the build instead of
// quietly leaving the datastore.
func Coverage(catalog *tcgplayer.CatalogDump, singles []string, printings map[int][]string) map[int][]string {
	out := map[int][]string{}
	for _, product := range catalog.Products {
		if !slices.Contains(singles, product.ProductType) {
			continue
		}
		if len(printings[product.ProductID]) == 0 {
			continue
		}
		out[product.ProductID] = printings[product.ProductID]
	}
	return out
}

// LogCoverage says how many of the contract's products the build carried.
func LogCoverage(carried int, finishes map[int][]string) {
	log.Printf("coverage: %d of %d catalog card products carried, %d skipped",
		carried, len(finishes), len(finishes)-carried)
}

// ProductsIn counts the products in each group.
func ProductsIn(products []tcgplayer.Product) map[int]int {
	out := map[int]int{}
	for _, product := range products {
		out[product.GroupID]++
	}
	return out
}

// SetTally is what Sets counted on its way through the groups.
type SetTally struct {
	// Populated is how many groups hold a product, and so emit a set.
	Populated int
	// Promo is how many of them are typed promo.
	Promo int
	// Empty is how many hold no product and were skipped.
	Empty int
}

// Sets are the catalog groups that hold anything, each under its code, typed
// promo where promo says the group is wholly promotional: the type is what
// tells the matcher a printing is. A group with no product is a husk
// TCGplayer keeps around for a set it has not stocked yet, and a set nothing
// references is dead weight in every consumer. Its code stays claimed, so no
// existing set's code moves while it is empty.
func Sets(catalog *tcgplayer.CatalogDump, codes map[int]string, promo func(tcgplayer.Group) bool) (map[string]*emit.Set, SetTally) {
	productsIn := ProductsIn(catalog.Products)
	sets := map[string]*emit.Set{}
	var tally SetTally
	for _, group := range catalog.Groups {
		if productsIn[group.GroupID] == 0 {
			tally.Empty++
			continue
		}
		tally.Populated++
		set := &emit.Set{Name: group.Name, ReleaseDate: group.ReleaseDate()}
		if promo(group) {
			set.Type = "promo"
			tally.Promo++
		}
		sets[codes[group.GroupID]] = set
	}
	return sets, tally
}

// Log says how many sets are promotional and how many groups were skipped,
// and refuses a build whose sets are fewer than its populated groups.
func (t SetTally) Log(sets map[string]*emit.Set) {
	t.LogPromo(sets)
	t.LogEmpty()
	t.Recount(sets)
}

// LogPromo says how many of the sets are promotional.
func (t SetTally) LogPromo(sets map[string]*emit.Set) {
	log.Printf("promotional sets: %d of %d", t.Promo, len(sets))
}

// LogEmpty says how many groups held no product, when any did.
func (t SetTally) LogEmpty() {
	if t.Empty > 0 {
		log.Printf("sets: %d empty groups hold no product and are skipped", t.Empty)
	}
}

// Recount refuses a build with fewer sets than populated groups. A code
// claimed twice would fold two groups onto one entry, and validate cannot
// see it - the code still resolves for every card naming it, it just names
// the wrong set.
func (t SetTally) Recount(sets map[string]*emit.Set) {
	if len(sets) != t.Populated {
		log.Fatalf("emitted %d sets for %d populated catalog groups; refusing to publish",
			len(sets), t.Populated)
	}
}

// Shelf says where a sealed product is filed: its set code and release date.
type Shelf func(tcgplayer.Product) (code, releaseDate string)

// GroupShelf files a sealed product in its catalog group's set, dated by the
// group.
func GroupShelf(groupByID map[int]tcgplayer.Group, codes map[int]string) Shelf {
	return func(product tcgplayer.Product) (string, string) {
		group := groupByID[product.GroupID]
		return codes[group.GroupID], group.ReleaseDate()
	}
}

// SealedEntries are the sealed products' entries in product id order, each
// filed by shelf and identified by stem of its set code and its product id.
// It sorts products in place.
func SealedEntries(products []tcgplayer.Product, shelf Shelf, stem func(string) string) []*emit.Sealed {
	sort.Slice(products, func(i, j int) bool {
		return products[i].ProductID < products[j].ProductID
	})
	sealed := slices.Grow([]*emit.Sealed(nil), len(products))
	for _, product := range products {
		code, releaseDate := shelf(product)
		sealed = append(sealed, &emit.Sealed{
			ID:            fmt.Sprintf("%s-%d", stem(code), product.ProductID),
			Name:          product.Name,
			SetCode:       code,
			ReleaseDate:   releaseDate,
			Image:         emit.ImageURL(product.ImageURL),
			ExternalLinks: emit.Links{TCGPlayerID: product.ProductID},
		})
	}
	return sealed
}

// Contract is what Publish checks a document's re-read against.
type Contract struct {
	// Finishes is the coverage contract, from Coverage.
	Finishes map[int][]string
	// Split is the catalog's products as the build split them.
	Split Split
	// Carried is how many card products the entries carry.
	Carried int
}

// Publish encodes the document, re-reads it against the coverage contract
// and the game's rules, recounts the sealed side, and writes it once the
// baseline guard lets it through.
func (b *Build) Publish(doc *emit.Datastore, c Contract) {
	buf := b.Encode(doc)

	// Re-read the encoded output and verify it structurally before
	// publishing anything: a format drift or a truncated download must
	// fail here, not in every consumer.
	counted, err := validate.Datastore(buf.Bytes(), c.Finishes, b.Rules)
	if err != nil {
		log.Fatalln("validation:", err)
	}
	log.Printf("validated: %d sets, %d cards, %d sealed", counted.Sets, counted.Cards, counted.Sealed)
	if counted.Cards != len(doc.Cards) || counted.Sealed != len(doc.Sealed) {
		log.Fatalf("emitted %d cards, %d sealed but read back %d, %d; refusing to publish",
			len(doc.Cards), len(doc.Sealed), counted.Cards, counted.Sealed)
	}
	// The coverage contract for the sealed side. Sealed is everything the
	// catalog does not type as a single, so it cannot lose a product to a
	// rule that did not know what to do with it; it can lose one to an
	// edit, which the card side's invariant is blind to.
	wantSealed := c.Split.Products - c.Carried - len(c.Split.Unpriced)
	if counted.Sealed != wantSealed {
		log.Fatalf("%d sealed products emitted but the catalog types %d as something other than a card; refusing to publish",
			counted.Sealed, wantSealed)
	}
	b.Write(buf)
}

// Encode reports the hand tables, wraps the document in the envelope and
// encodes it, spells its quotes the way a query does, and sets aside any
// token the vocabulary refuses, logged, so what it returns is what will be
// published.
func (b *Build) Encode(doc any) *bytes.Buffer {
	handtable.Report()

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(emit.Envelope(emit.Today(), doc)); err != nil {
		log.Fatalln(err)
	}
	emit.PlainQuotes(&buf)
	// A token the vocabulary refuses comes off its printing here rather than
	// stopping every card of the game at the publish's check.
	setAside, err := vocabulary.FitInto(&buf)
	if err != nil {
		log.Fatalln(err)
	}
	for _, line := range setAside {
		log.Println("vocabulary:", line)
	}
	return &buf
}

// Write compares the encoded build against the baseline, when the publish
// handed one over, says whether it is fit to become the next one, and
// writes it.
func (b *Build) Write(buf *bytes.Buffer) {
	count := b.Count
	if count == nil {
		count = baseline.Count
	}
	if err := baseline.Guard(buf.Bytes(), count, baseline.Options{
		Against: b.Against, Tolerance: b.Tolerance, FitPath: b.BaselineFit, Unit: b.Unit,
	}); err != nil {
		log.Fatalln(err)
	}

	if b.Output == "" {
		if _, err := os.Stdout.Write(buf.Bytes()); err != nil {
			log.Fatalln(err)
		}
		return
	}
	f, err := os.Create(b.Output)
	if err != nil {
		log.Fatalln(err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		log.Fatalln(err)
	}
	// A short write on a full disk can surface only here.
	if err := f.Close(); err != nil {
		log.Fatalln(err)
	}
}
