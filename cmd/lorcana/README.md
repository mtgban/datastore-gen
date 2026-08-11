# lorcana-datastore

Builds the Lorcana datastore file consumed by
[go-mtgban](https://github.com/mtgban/go-mtgban)'s `mtgmatcher/lorcana`
loader, and publishes it to B2 for the workers to pull.

The datastore is the [LorcanaJSON](https://lorcanajson.org) allCards payload,
enriched with what our TCGplayer catalog dump for category 71 knows and it
does not:

- the product id on cards upstream publishes none for, when exactly one
  unclaimed catalog product matches by name and collector number;
- the extra product ids TCGplayer uses for a card's foil, which it sells as
  a separate product (`tcgPlayerExtraIds`);
- the TCGplayer printing names each card is sold under
  (`tcgPrintings`: Normal, Holofoil, Cold Foil), beside LorcanaJSON's own
  richer foil sub-types, with disagreements between the two reported at
  build time;
- every sealed product the catalog carries, in a top-level `sealed` array a
  stock LorcanaJSON reader ignores, with a set entry minted for the groups
  LorcanaJSON has no set for.

Card identity is left entirely to LorcanaJSON: its integer card ids are the
matcher's uuids, and its foil sub-type names are what storefront wording
resolves against. The promotional printings TCGplayer files in their own
groups are mapped onto the LorcanaJSON printings they decorate by name and
number, never minted as new cards.

The output is the LorcanaJSON payload itself with the extra data merged in,
so the loader reads it unchanged and a stock LorcanaJSON reader still parses
it. Before writing anything the result is re-read, structurally verified,
and checked against a minimum card count (`-min-cards`, default 3000), so a
broken upstream payload can never be published.

This repository is deliberately standalone: it produces JSON and depends on
nothing, so a datastore change never waits on a go-mtgban release. The few
helpers the loader also has are duplicated here instead of imported.

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
- `-min-cards` — refuse to emit a datastore with fewer cards (default 3000)

## Publishing

The `publish` workflow builds and uploads
`b2://mtgban-datastore/lorcana/lorcana.json.xz` daily. Consumers should
point their `LORCANA_PATH` at that object.
