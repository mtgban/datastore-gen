# cmd/riftbound

Builds the Riftbound datastore file consumed by
[go-mtgban](https://github.com/mtgban/go-mtgban)'s `mtgmatcher/riftbound`
loader. See the [repository README](../../README.md) for how the builders
are run and published.

The tool merges two sources:

- the official card gallery payload from riftbound.leagueoflegends.com
  (resolving the current site build id automatically), which provides the
  main sets;
- our own TCGplayer catalog dump for category 89, written by tcgdumper and
  published beside the datastore, passed in with `-tcg-catalog`. It provides
  the TCGplayer product id stamped onto every printing (feeding the matcher's
  external identifier index), the printings the gallery does not carry, the
  finishes each printing is sold in, and every sealed product.

The gallery says which printings are published, never which products exist.
Every product the catalog types as a card becomes a printing:

- a product whose collector number the gallery already carries stamps its
  id onto that printing;
- a product the gallery has no printing for — the rune variants, the
  dual-faced tokens the gallery files one row per face of — is adopted into
  the set on the catalog's word, with the parenthetical qualifiers of its
  name becoming promo types;
- a group the gallery has no set for is a set of its own rather than a
  group to skip, typed `promo` only when the group actually hands out
  promotional printings, so a set the gallery has merely not published yet
  stays a main set.

`validate` re-reads the encoded output and refuses to publish if any catalog
card product carries no printing, if two printings claim one product, or if
two sets wear one id — the last of which would fold two groups onto one set
while every card naming it still resolved.

The gallery says nothing about finish, and most of Riftbound is sold in one
finish only - promotional printings foil, starter cards plain - so the
finishes come from the printings the catalog lists for a product. The dump is
used rather than a price feed because it names a printing whether or not
anyone is selling it today.

The output is the gallery payload itself with the extra data merged into the
gallery blade, so the loader reads it unchanged. Before writing anything the
result is re-read and structurally verified, and compared against the last
build (see the [repository README](../../README.md)), so a broken upstream
payload can never be published.

This builder is deliberately standalone: it produces JSON and depends on
nothing but the catalog reader, so a datastore change never waits on a
go-mtgban release. The few helpers the loader also has are duplicated here
instead of imported.

## Usage

```sh
go run . -tcg-catalog tcgplayer-catalog.json -o riftbound.json
```

- `-tcg-catalog` (required) — a tcgdumper dump of TCGplayer category 89
- `-gallery` — a saved card-gallery payload (the live gallery by default)
- `-o` — output file (stdout by default)
- `-against` — the baseline to compare this build against, with
  `-against-tolerance` and `-baseline-fit`

## License

MIT, see [LICENSE](../../LICENSE). Card data, images, and trademarks belong
to Riot Games; this project is not affiliated with or endorsed by Riot Games.
