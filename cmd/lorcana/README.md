# cmd/lorcana

Builds the Lorcana datastore file consumed by
[go-mtgban](https://github.com/mtgban/go-mtgban)'s `mtgmatcher/lorcana`
loader. See the [repository README](../../README.md) for how the builders
are run and published.

The datastore is the [LorcanaJSON](https://lorcanajson.org) allCards payload,
enriched with what our TCGplayer catalog dump for category 71 knows and it
does not:

- the product id on cards upstream publishes none for, when exactly one
  unclaimed catalog product matches by name and collector number, and the
  removal of one upstream put on two cards from the card the product does
  not identify, since two cards sharing an id merge their price histories;
- the extra product ids TCGplayer uses for a card's foil, which it sells as
  a separate product (`tcgPlayerExtraIds`);
- the TCGplayer printing names each card is sold under
  (`tcgPrintings`: Normal, Holofoil, Cold Foil), beside LorcanaJSON's own
  richer foil sub-types — and the catalog settles which of them exist, a
  printing TCGplayer prices a sku for being one that exists, while upstream
  keeps naming the foils, since its sub-types are what storefront wording
  resolves against;
- every sealed product the catalog carries, in a top-level `sealed` array a
  stock LorcanaJSON reader ignores, with a set entry minted for the groups
  LorcanaJSON has no set for.

The promotional printings TCGplayer files in their own groups (DLPC, D23,
D100) are matched onto upstream's own cards wherever the id fill can do it
by name and number, because upstream files them under the set they belong
to and its card is the better one. What no card claims or matches is minted
here rather than dropped: a product TCGplayer sells is a printing that
exists, and a datastore leaving it out leaves every listing of it
unresolvable. A minted card is filed under the negated product id, so it
cannot collide with an id upstream publishes later, and the day upstream
publishes the real card its own entry claims the product and the minted one
stops being minted.

The result is the union of both sources: every card LorcanaJSON publishes,
and every product the catalog types as a card. `validate` re-reads the
encoded output and refuses to publish if any catalog card product carries
no card.

Card identity is left entirely to LorcanaJSON: its integer card ids are the
matcher's uuids, and its foil sub-type names are what storefront wording
resolves against. Set codes are LorcanaJSON's too; a catalog group whose
abbreviation another group already claimed gets its own group id suffixed,
so two groups can never fold onto one set.

The output is the LorcanaJSON payload itself with the extra data merged in,
so the loader reads it unchanged and a stock LorcanaJSON reader still parses
it. Before writing anything the result is re-read and structurally verified,
and compared against the last build (see the [repository
README](../../README.md)), so a broken upstream payload can never be
published.

This builder is deliberately standalone: it produces JSON and depends on
nothing but the catalog reader, so a datastore change never waits on a
go-mtgban release. The few helpers the loader also has are duplicated here
instead of imported.

## Usage

```sh
go run . -tcg-catalog tcgplayer-catalog.json \
  -lorcana https://lorcanajson.org/files/current/en/allCards.json \
  -o lorcana.json
```

- `-tcg-catalog` (required) — a tcgdumper dump of TCGplayer category 71,
  published nightly at `b2://mtgban-datastore/lorcana/tcgplayer-catalog.json.xz`
- `-lorcana` (required) — the LorcanaJSON allCards file, as a path or URL
- `-o` — output file (stdout by default)
- `-against` — the baseline to compare this build against, with
  `-against-tolerance` and `-baseline-fit`
