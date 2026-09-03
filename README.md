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
- cmd/gundam - Gundam Card Game, from the category 86 catalog dump
  annotated with yzRobo's gcg-api mirror of the Bandai card list
- cmd/palworld - Palworld OFFICIAL CARD GAME, from the category 91 catalog
  dump annotated with palworldtcg.gg's card API

## Every datastore is the sum of both sources

A datastore holds every product the catalog types as a card *and* every
card the upstream dataset publishes. Neither source alone is the answer:
TCGplayer sells printings no card database has published yet, and the
games print cards - tokens, trainer kits, regional promos - that TCGplayer
never lists as singles. A datastore missing either half leaves every
listing of the missing printings unresolvable.

The two halves are reached differently depending on which source carries
identity.

**The catalog carries the identity** (gundam, palworld). Both join a
community mirror of the publisher's card list - yzRobo/gcg-api for Gundam,
palworldtcg.gg's public API for Palworld - and take from it only what the
catalog cannot supply: the cards the game prints and TCGplayer sells no
single of. Those are minted, naming no product because none exists, and
they come to 10 entries for Gundam and one for Palworld, all of them the
resource and token cards handed out with decks. No upstream image or rules
text is stored: gcg-api publishes under no clear licence, so what is taken
is the fact that a card exists and the identity it exists under.

Palworld's two sources number the same card differently, and the join sets
the difference aside rather than picking a winner. Bushiroad's English site
serves the card as EBP01-001 and its Japanese site as BP01-001, so the
prefixed form is what is printed on the card TCGplayer sells; every number
this datastore publishes wears it, minted rows included.

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
| onepiece | 18 pre-errata printings, hand-carried | 0 |

One Piece mints from neither source: the catalog carries every number
Bandai publishes, and Bandai files an errata as a correction to a card
rather than as a new printing, so neither knows the pre-errata print runs
that collectors and marketplaces price separately. Those 18 are carried by
hand in `cmd/onepiece`, keyed on CardTrader's blueprint id and reading
finish, artwork and rarity from the printing each is an errata of. A row
stands down the day any source carries its identity, so the hand-carried
entry never becomes a second card wearing a name a product already holds.

Yu-Gi-Oh is the exception: it has no card-level upstream source at all.
YGOPRODeck's cardinfo.php is deliberately not fetched - their terms forbid
hotlinking - so only set release dates are joined, and there is no second
half to add.

## What a datastore carries

Cards, and the sealed product that holds cards. Not accessories: no coins,
dice, pins, sleeves, playmats, binders, portfolios or deck boxes, and none
of the empty boxes and tins sold once the cards are out of them. A thing
holding no card is not this datastore's to price.

That holds today without a rule enforcing it, and it holds structurally: the
sealed side is whatever a game's own category dump does not type as a card,
and TCGplayer files supplies under a category of their own, so they never
reach a builder. All six games were checked in 2026-09: no accessory-only
entry in any of them.

It is worth saying anyway, because the obvious way to enforce it is wrong. A
pattern matching accessory words against sealed names flags 130 entries and
128 of them are genuine product - "Collector's Pin Three Pack Blisters
(Chespin)" is three booster packs and a pin, and the pin is why the name
says pin. The test that matters is whether the product holds cards, never
what its name mentions.

The vendors do sell accessories, and a coverage report comparing their
shelves against this datastore will count them as things it lacks. It does
not lack them.

## The zero-skip invariant

Every builder re-reads its own encoded output and checks it before writing
anything. The core check is coverage: the products the emitted entries
carry must be exactly the products the catalog types as a card. A product
no rule knew what to do with stops the publish instead of quietly leaving
the datastore. Set codes are checked the same way - a code claimed twice
would fold two groups onto one set, so the builders mint unique codes and
refuse to publish a set count that does not match the group count.

## Refusing a build that lost something

There used to be a `-min-cards` floor per game, a number invented once and
never revisited, sitting so far below what the datastores actually hold
that a build could lose a third of itself and still publish. It is gone.
What replaces it is `-against <baseline>`, which compares this build
against the last one and needs no number maintained by anyone.

Only shrinkage is suspicious - these datastores grow every week - and only
three shapes of it are refused:

- a card or sealed total that fell by more than `-against-tolerance`
  (1% by default);
- a set that holds no card at all any more;
- a set that lost more than half of what it held.

The last two are what a whole-file count cannot see. One set folding onto
another - the bug the unique set codes now prevent - moves the total by a
fraction of a percent while emptying a set completely. Every other per-set
drop is logged rather than refused, because a product delisted here and
there is ordinary and a check that cried wolf would be turned off.

### The catalog decides the finishes

A printing TCGplayer prices a sku for is one that exists, so the catalog
settles which finishes a card has, in every game. Four of the builders get
this for free: onepiece, yugioh, fleshandblood and pokemon emit one entry
per catalog sku printing, so no upstream dataset has any say. cmd/riftbound
takes them from the catalog too and says so out loud on the only case that
could fall back to the gallery's own. cmd/lorcana is the one that had to be
taught: it builds on LorcanaJSON, whose foilTypes drive the loader's uuids,
so a card upstream called foil-only got no nonfoil uuid and every nonfoil
listing of it resolved to nothing while TCGplayer sold one. The catalog now
settles which finishes exist and upstream still names the foils, its
sub-types being what storefront wording resolves against.

### The baseline only moves forward

The baseline is its own object, `<game>/<game>.baseline.json.xz`, and not
simply the datastore the run is about to replace. A build that comes in a
little smaller still publishes, but does not become the baseline: were it
to, a run of individually tolerated drops would ratchet the baseline down a
step a night, and the whole loss would never be large enough for any single
run to see. Measuring from the high-water mark instead means the drift has
to stay under the tolerance in total, not per night.

`-baseline-fit <path>` is how the builder says so: it writes that file only
when the build holds at least as much as the baseline it was measured
against, and the workflow promotes the build to the baseline only when the
file is there.

A game whose baseline has not been written yet is seeded from the published
datastore. A game with nothing published at all has neither, and builds
without a baseline that first time rather than failing. A baseline left
standing above a genuine, lasting shrinkage is reset by running the publish
workflow with `rebaseline` set, which ignores it for that run and promotes
whatever the build holds.

## Usage

Every builder takes the catalog dump with `-tcg-catalog` and writes to
`-o` (stdout by default). The upstream source defaults to its public
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
live API, and `-tcgdex-cache <dir>` to keep the last good responses: the
live API is asked first and refreshes the cache whenever it answers, and
when it is unreachable the cached responses stand in, dated in the log,
rather than the publish being lost — the workflow keeps that cache in the
bucket beside the datastore. `cmd/fleshandblood` takes `-fab-cards` and
`-fab-sets`, and `cmd/riftbound` takes `-gallery` to read a saved
card-gallery payload. Every builder takes `-against`,
`-against-tolerance` and `-baseline-fit`.

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
