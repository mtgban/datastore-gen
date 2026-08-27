# datastore-gen

Builders for the mtgban game datastores. Each command ingests the TCGplayer
catalog dump for its game, joins it against a public card dataset for that
game, and emits one JSON datastore, published to
b2://mtgban-datastore/<game>/<game>.json.xz for go-mtgban and the website to
consume.

The builders are standalone on purpose: no dependency on go-mtgban, no
external modules beyond the catalog reader, types duplicated rather than
imported, so a datastore change never drags a library upgrade behind it.

- cmd/riftbound - Riftbound (League of Legends TCG), from the official
  card gallery and the category 89 catalog dump
- cmd/lorcana - Disney Lorcana, from LorcanaJSON and the category 71
  catalog dump
- cmd/onepiece - One Piece Card Game, from the category 68 catalog dump
  annotated with punk-records' mirror of the official Bandai card list
- cmd/yugioh - Yu-Gi-Oh, from the category 2 catalog dump with release
  dates filled from YGOPRODeck's set list
- cmd/fleshandblood - Flesh and Blood, from the category 62 catalog dump
  annotated with the-fab-cube's card dataset
- cmd/pokemon - Pokemon, from the category 3 catalog dump annotated with
  the tcgdex card database

## Every datastore is the sum of both sources

A datastore holds every product the catalog types as a card *and* every
card the upstream dataset publishes. Neither source alone is the answer:
TCGplayer sells printings no card database has published yet, and the
games print cards - tokens, trainer kits, regional promos - that TCGplayer
never lists as singles. A datastore missing either half leaves every
listing of the missing printings unresolvable.

The two halves are reached differently depending on which source carries
identity.

**Upstream is the datastore** (riftbound, lorcana). The output is the
upstream payload itself with the catalog's data merged in, so every
upstream card is carried by construction, and the products upstream has no
card for are minted alongside it.

**The catalog is the datastore** (onepiece, fleshandblood, pokemon). Every
entry is one priced printing of a catalog product, and the upstream dataset
supplies both annotation - a printing id, a clean image, a release date -
and the cards the catalog has no product for, which are minted from it.

A minted entry names no TCGplayer product, because none exists; nothing
prices it, and the loaders group an entry without a product id by its own
id with the finish suffix stripped. Where the upstream set has no catalog
group at all, the set is minted too, from upstream's own code, name and
release date, deduplicated against the codes the catalog groups already
hold. Current counts:

| builder | upstream cards minted | sets minted |
|---|---|---|
| lorcana | 203 catalog products upstream has no card for | 0 |
| fleshandblood | 466 entries over 418 collector numbers | 36 |
| pokemon | 909 entries over 883 tcgdex cards | 39 |
| onepiece | none — the catalog carries every number Bandai publishes | 0 |

Yu-Gi-Oh is the exception: it has no card-level upstream source at all.
YGOPRODeck's cardinfo.php is deliberately not fetched - their terms forbid
hotlinking - so only set release dates are joined, and there is no second
half to add.

## The zero-skip invariant

Every builder re-reads its own encoded output and checks it before writing
anything. The core check is coverage: the products the emitted entries
carry must be exactly the products the catalog types as a card. A product
no rule knew what to do with stops the publish instead of quietly leaving
the datastore. Set codes are checked the same way - a code claimed twice
would fold two groups onto one set, so the builders mint unique codes and
refuse to publish a set count that does not match the group count.

## Usage

Every builder takes the catalog dump with `-tcg-catalog` and writes to
`-o` (stdout by default), and refuses to emit a datastore with fewer card
entries than `-min-cards`. The upstream source defaults to its public
location and can be overridden with a path or URL:

```sh
go run ./cmd/riftbound      -tcg-catalog tcgplayer-catalog.json -o riftbound.json
go run ./cmd/lorcana        -tcg-catalog tcgplayer-catalog.json -lorcana allCards.json -o lorcana.json
go run ./cmd/onepiece       -tcg-catalog tcgplayer-catalog.json -o onepiece.json
go run ./cmd/yugioh         -tcg-catalog tcgplayer-catalog.json -o yugioh.json
go run ./cmd/fleshandblood  -tcg-catalog tcgplayer-catalog.json -o fleshandblood.json
go run ./cmd/pokemon        -tcg-catalog tcgplayer-catalog.json -o pokemon.json
```

`-lorcana` is the one required upstream flag; the others default to their
public URLs. `cmd/pokemon` additionally takes `-tcgdex-sets` and
`-tcgdex-cards` to read saved GraphQL responses instead of querying the
live API, `cmd/fleshandblood` takes `-fab-cards` and `-fab-sets`, and
`cmd/riftbound` takes `-gallery` to read a saved card-gallery payload.

The dump itself is written by tcgdumper
(github.com/mtgban/go-tcgplayer/cmd/tcgdumper) and published nightly beside
each datastore at `b2://mtgban-datastore/<game>/tcgplayer-catalog.json.xz`.

## Publishing

The `publish` workflow runs daily at 07:00 UTC, two hours after the catalog
dump, and on demand for one game or all of them. It downloads the dump from
B2, builds the datastore, compresses it, and uploads it to
`b2://mtgban-datastore/<game>/<game>.json.xz`. Consumers decompress by
suffix, so the extension matters. It needs the secrets
`B2_APPLICATION_KEY_ID_DATASTORE` and `B2_APPLICATION_KEY_DATASTORE`, an
application key allowed to write that bucket, and reads the repository
variable `DATASTORE_LORCANA` for the LorcanaJSON location.

## License

MIT, see [LICENSE](LICENSE). Card data, images, and trademarks belong to
their respective publishers; this project is not affiliated with or
endorsed by any of them.
