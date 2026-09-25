# datastore-gen — Specification

What the builders read, what they publish, and what every build is held
to. `AGENTS.md` says how to work here; this document says what "here" is.
Facts below were read off the code on 2026-09-14, and §2's envelope on
2026-09-24 (master at `7bdb114`, plus this branch); where a number is
quoted it is the earlier day's.

Module `github.com/mtgban/datastore-gen`, Go 1.25. Direct dependencies:
`github.com/mtgban/go-tcgplayer` (the catalog reader) and
`github.com/mtgban/go-cardmarket` (the Cardmarket catalog reader, Pokemon
only). Nothing from go-mtgban is imported; the consumer and the producer
meet only at the published file.

## 1. What a datastore is

One JSON document per game, published at
`b2://mtgban-datastore/<game>/<game>.json.xz`, holding every product the
TCGplayer catalog types as a card *and* every card the game's upstream list
publishes, plus the sealed product that holds cards. Neither source alone is
the answer: TCGplayer sells printings no database has published yet, and
the games print cards TCGplayer never lists as singles.

The join runs one of three ways, and it decides what an entry is:

| shape | games | an entry is |
|---|---|---|
| the catalog is the datastore | onepiece, fleshandblood, pokemon | one priced sku printing of one catalog product; upstream annotates and mints the cards the catalog lacks |
| the catalog carries the identity | gundam, palworld | one priced sku printing; upstream supplies only the cards TCGplayer sells no single of |
| upstream is the datastore | riftbound, lorcana | the upstream payload itself with the catalog merged in; catalog products upstream lacks are minted beside it |

Yu-Gi-Oh mints nothing: YGOPRODeck lends passcodes and release dates and no
card the catalog does not sell.

A **minted** entry names no TCGplayer product, because none exists; nothing
prices it, and the loader groups its printings by the upstream id it was
minted from (`fabId`, `tcgdexId`, `cardmarketId`, the gallery id). Minted
counts today: lorcana 212 products, fleshandblood 203 numbers, pokemon 257
entries over 867 tcgdex cards plus the Cardmarket-only stamped promos,
onepiece 58 pre-errata printings hand-carried from CardTrader and 39
minted from Cardmarket's pre-errata shelf, gundam 7 tokens and 8
hand-carried promotional reprints.

## 2. The document

One envelope per file: `meta` says when the file was built and which
schema it is, `data` holds the document. The shape is MTGJSON's own idiom
(`mtgmatcher/magic`'s `AllPrintings{Meta, Data}`), spelled once in
`emit.Envelope` for all eight builders.

```json
{
  "meta": {
    "date":    "2026-09-14",
    "version": "1"
  },
  "data": {
    "game":   "pokemon",
    "sets":   { "<setCode>": { ...set } },
    "cards":  [ { ...card } ],
    "sealed": [ { ...sealed } ]
  }
}
```

`meta` is encoded from a struct, not a map, so it is written first: the
version exists to be read before the rest is decoded, and a sorted map put
it in the last sixty bytes of the file behind everything it qualifies.
`meta.date` is the build's own date, UTC (`emit.Today`); `meta.version` is
`emit.SchemaVersion`, and moves only when what `data` holds changes in a
way a reader must know about before it decodes.

`meta` says nothing else. The game in particular stays in the document,
where it has always been and where the loaders' wrong-file guard reads it:
nothing detects a game from a file, callers name the game to `Open`, and
putting it in `meta` would split one fact across two places and buy nothing.

`data` holds exactly what used to sit at the top level, unchanged: a reader
that unwraps `data` decodes what it always did. Every game but Riftbound
and Lorcana writes the `game`/`sets`/`cards`/`sealed` shape above; those
two put their upstream payload there whole, and are in §2.5.

Before encoding, `emit.PlainQuotes` rewrites every typographic quote in the
payload to its ASCII form, so the check that re-reads the file sees what is
published and a query spells names the way the file does; the envelope goes
around it afterwards, and `meta` carries no prose to rewrite.

Every reader here peels through `emit.Unwrap`, or `emit.UnwrapDocument`
where it holds the document already decoded, and none spells the peel
again. A file that is not an envelope is refused (`emit.ErrNotEnvelope`):
it is an upstream payload or a build from before the envelope, and reading
it as the document would measure a build against the wrong thing.

A document is an envelope when it carries **both** `meta` and `data`, both
objects. `data` alone is not the test: a document is free to publish a
field of its own by that name, and peeling on the key alone would re-root
a reader into it and lose the real document without saying so. None of the
eight games publishes either key bare today — Lorcana's upstream carries
`metadata`, which is a different name — and asking for both keeps that
true should one ever start. A `meta.version` this build does not know is
refused (`emit.ErrUnknownSchema`), not decoded on the chance that `data`
still reads: that refusal is what writing the version first is for.

Reading both shapes was a migration cost, and it ended on the condition
this section set for it rather than on a date: every game has published
under the envelope since 2026-09-21, and the last bare baseline, pokemon's,
was rebuilt on 2026-09-24. Nothing bare can arrive any more, so `Unwrap`
refuses a document that is not an envelope instead of handing it back
whole.

### 2.1 Sets

Keys are set codes: the catalog group's abbreviation, upper case, repaired
where the catalog leaves it blank or claims it twice (a derived code, or the
abbreviation with `-<groupId>`). A code is never claimed twice; a build whose
set count differs from its group count is refused.

| field | meaning |
|---|---|
| `name` | the catalog group's name (Pokemon strips a leading code such as `SWSH07:`) |
| `releaseDate` | `YYYY-MM-DD`, the group's, or YGOPRODeck's where the catalog's is a placeholder |
| `type` | `"promo"` on the sets that hand out promotional printings; absent otherwise |
| `baseSetSize` | the set's printed size; only where the game prints or publishes one (see §3.4) |
| `abbreviation`, `symbol` | Pokemon: the code and the tcgdex/pokemontcg.io symbol URL |
| `collectorNumberMax` | Riftbound, from the gallery |

A set upstream publishes and the catalog has no group for is **minted**
from upstream's own code, name and date (lorcana, fleshandblood, pokemon,
riftbound). A set holding neither a card nor a sealed product is refused
(pokemon) or never emitted.

### 2.2 Cards

Common to every game:

| field | meaning |
|---|---|
| `id` | the entry's uuid, see §2.4; unique across the file |
| `name` | the card's name, as the game's own list spells it; parentheticals that are part of the name stay (`"Dark Magician (Arkana)"`, `"Unicorn Gundam (Destroy Mode)"`) |
| `setCode` | key into `sets` |
| `number` | the collector number as the card prints it, without the printed total (`"SWSH252"`, `"GD03-057"`, `"072"`, `"Z"`); a string in every game, because an absent number and a printed `0` are different facts and an integer spells them alike; absent on the few products the catalog files with none |
| `total` | Pokemon and Lorcana: the printed total behind the slash (`"167"`, `"204"`, Lorcana's promo runs `"P1"`), kept apart because it is the set's fact and the only thing telling `8/102` from `8/130`, or `1/204` from `1/P1` |
| `rarity` | the catalog's rarity, corrected where the catalog's own name says another (Yu-Gi-Oh reads a rarity written into a name as the rarity) |
| `finish` | the catalog's printing name for the sku this entry prices: `Normal`, `Holofoil`, `Reverse Holofoil`, `1st Edition`, `Unlimited`, `Rainbow Foil`, `Cold Foil`… One entry per sku printing; the catalog decides which exist |
| `variant` | the catalog's qualifiers, joined with spaces, wording untouched: the prose everything below is distilled from |
| `promoTypes` | the promotions the printing wears, as lowercase slug tokens (§3.1); absent when none |
| `watermark` | the mark: which copy of this number the printing is (§3.2); absent when none |
| `language` | the printing's language where it is not the game's default (`"Japanese"`, `"German"`) |
| `originalReleaseDate` | a date a label stated that the set's date does not cover (§3.3) |
| `image` | the catalog image at the 400-wide rendition; Pokemon and Lorcana also carry `images.{full,thumbnail}` |
| `externalLinks` | `tcgPlayerId` on every priced entry; the upstream id beside it or instead of it: `fabId`, `tcgdexId`, `cardmarketId`, `bandaiId`, `konamiId`, `cardTraderId` |
| `color`, `type`, `attribute`, `artist` | game facts where the source carries them: Flesh and Blood pitch colour, Gundam and Palworld colour and card type, Yu-Gi-Oh attribute, One Piece colour and type |

Per game, on top of the common keys: under `externalLinks`, `fabId`
(fleshandblood), `tcgdexId` (pokemon), `bandaiId` (onepiece) and `konamiId`
(yugioh, only where the passcode join is unambiguous and not contradicted
by the name); `printings[]` (lorcana, §2.5).

### 2.3 Sealed

`id`, `name`, `setCode`, `releaseDate`, `image`, `externalLinks.tcgPlayerId`.
The sealed side is every catalog product the game's category does not type
as a card: boosters, decks, bundles, tins that hold cards. No accessories
reach a builder, structurally, because TCGplayer files supplies under a
category of their own. Do not add a name filter for accessories; a check
in 2026-09 found 128 of 130 names such a filter flags are real product.

### 2.4 Ids

An id is generative: spelled from the number, the product and the printing
by rule, every build, with no map behind it. The loader treats it as
opaque. The spellings per game, with `<suffix>` = `emit.FinishSuffix` of
the printing name (`""` for the plain printing, else `_` plus the slug:
`_holofoil`, `_1stedition`, `_rainbowfoil`):

| game | priced entry | minted entry |
|---|---|---|
| fleshandblood | `<number>_<productId><suffix>` (`her156_664534_rainbowfoil`) | `<number><suffix>` (`her156_rainbowfoil`) |
| gundam | `<number>_<productId><suffix>` (`gd03-057_673481`) | `<number>` alone; hand-carried reprints `<number>-<label><suffix>` (`gd01-073-premium-card-collection-02_holofoil`) |
| onepiece | `<number>_<productId><suffix>` | hand-carried: `<number>_ct<blueprint><suffix>`; Cardmarket: `<number>_mkm<cardmarketId><suffix>` |
| palworld | `<number>_<productId><suffix>` | `<number>` alone |
| pokemon | `<number>-<total>_<productId><suffix>` (`226-164_268081`, `sm04_147224`) | tcgdex: `<tcgdexId><suffix>`; Cardmarket: `<number>-<total>_mkm<cardmarketId><suffix>` |
| yugioh | `<number>_<productId><suffix>` (`blgg-en116_695673_1stedition`); nothing minted | — |
| riftbound | `<abbrev>-<productId>` for every printing and sealed item | same, keyed by the product |
| lorcana | integer: LorcanaJSON's card id; uuids per printing `1951`, `1951_coldfoil`, `1951_holofoil` | `-<productId>`, uuids `m-714954`, `m-714954_holofoil` |

A product with no collector number takes the bare product id as its stem.
The number in an id is lower-cased and its separators folded to dashes;
Yu-Gi-Oh keeps the number as written. The two namespaces of a game (with
and without `_<productId>`) can never collide, which is what lets a minted
entry stand beside a priced one without a check.

### 2.5 The two other shapes

**Riftbound** publishes the card-gallery payload itself with the catalog
merged in, under `data`. The cards are at
`data.pageProps.page.blades[].cards.items[]`; each item keeps the gallery's
own fields (`id`, `name`, `publicCode`, `set.value.{id,label}`,
`rarity.value.id`, `finishes`, `cardImage.url`) and gains `setCode`,
`number`, `image` and `externalLinks.tcgPlayerId`, the names every game
shares and the ones `mtgmatcher/riftbound` reads. A catalog product the
gallery has no row for is adopted as a printing with the same shape. The
sealed products at `blades[].sealed.items[]` are ours alone and carry only
the shared names: `id`, `name`, `setCode`, `releaseDate`, `image` and
`externalLinks.tcgPlayerId`. There is no `cards` key at any level;
`internal/vocabulary`, `internal/datastorediff` and `internal/baseline`
(through a reader the builder supplies) walk the path.

**Lorcana** publishes LorcanaJSON's card objects with the catalog merged in,
likewise whole under `data` — upstream's own `metadata` key travels with
them untouched, and is not the envelope's `meta`:
`externalLinks.tcgPlayerId` and `tcgPlayerExtraIds`, and a `printings[]`
array of `{finish, id, promoTypes}` with one uuid per finish the catalog sells;
upstream's `foilTypes` is folded into it and removed. The catalog decides
which finishes exist (a card upstream calls foil-only still gets a nonfoil
uuid when TCGplayer sells one); upstream still names the foil sub-type as a
`promoTypes` label. A promo on a TCGplayer promo shelf also carries the label
its product name qualifies it with ("(Puzzle Promo)", "(Store Championship
Participant)"), as a minted card always has. `baseSetSize` comes from upstream's `cardCounts.base`.
A card sits in the set of the catalog group that sells it: a promo on
TCGplayer's promo shelf (D23, DLPC, D100), not in the set upstream says it
is legal in, and a card the catalog sells nowhere in upstream's own set.

## 3. Field semantics that carry rules

### 3.1 `promoTypes`

The query vocabulary of promotions. Each token is `^[a-z0-9]+$`, at most
`vocabulary.TokenLimit` = 22 characters, spelled by `emit.PromoSlug`
(lower case, letters and digits, nothing else). The words behind a token
are the loader's to keep (`mtgmatcher/<game>/promolabels.go`); this side
publishes the token and keeps the words in `variant`.

What is a token and what is not, the same in every builder:

- **A rarity is not a label.** A qualifier equal to the entry's rarity is a
  restatement and goes; a qualifier naming *a* rarity is the rarity
  (Yu-Gi-Oh publishes it as `rarity`).
- **A finish is not a label.** `holo`, `nonholo`, `holofoil` on the sku
  they describe are the finish field; where two products share a number
  apart from finish, the wording becomes the mark.
- **A number is not a label.** Bare numbering, a deck place (`#42
  Charizard Stamped` is card 42 of the Charizard deck), a span
  (`SWSH287-290`), a restatement of the entry's own number.
- **A subject is not a promotion.** What the card pictures stays the
  variant it is and, where it is the only thing telling two printings
  apart, the mark: a Pokemon on a promo, a character on a DON!! card, a
  Gundam form or part, a Pal on a Soul card. Tested against the card names
  the datastore carries where the game names every printing alike.
- **A set the shelf reprints from is provenance, not promotion**, on the
  shelves that hold cards from everywhere (Deck and Blister Exclusives, the
  jumbo cards, the Burger King and World Collection promos, the miscellany),
  and a label that is a set name plus a promotion splits (`Team Up Stamped`
  is `stamped` with the set as the mark).
- **One label, one spelling.** Folded to the wording most listings arrive
  in; a tie folds nothing; the sealed side is the authority where the
  singles disagree; a hand table names the rest, and reports rows nothing
  uses.
- **A token names a promotion in its own right.** A set name in front of a
  promotion comes off at any length when what is left is a label the
  catalog writes on its own (`Prerelease`, `Stamped`), and is forced off
  past 22 characters.

The checks `internal/vocabulary.Check` runs on a built file: `NotSlugs`
(a token that is not a slug), `TooLong` (past 22 characters *and* holding a
published set name), `RarityEchoes`, `FinishEchoes`, and `Alike` (two
printings no published field tells apart).

### 3.2 `watermark`, the mark

Which copy of a collector number this printing is, where nothing else says:
the player whose World Championship deck a card came in, the set a shelf
reprinted it from, the character on a DON!! card, the Duelist League ink,
the deck and place of a Battle Academy stamp, the finish where two
products differ by nothing else, a print run (`red cheeks`, `black dot
error`), an energy kind, an instalment (`series 7`). Lower case, words and
spaces, the catalog's own wording. It is published wherever the label is
one of these by nature, and put back by the **collision guard** wherever a
dropped label turns out to be the only thing between two printings: every
drop is set aside rather than deleted, and the guard compares
`name|number|total|setCode|rarity|finish|promoTypes|watermark|date|language`
after the drops. The guard has caught something in every game it was
written for.

### 3.3 `originalReleaseDate`, `language`, `color`

A fact a fold would take away is published as a field rather than folded
away: the year in `World Championships 2013` (18 spellings of one promotion
otherwise), the language in `Japanese Meiji Chocolate Exclusive Promo`, the
ink in `Dark Magician Girl (Blue)`. The date is published only where the
set's own date does not already cover it, which is what makes it an
*original* release date; the TCGplayer catalog carries no per-product date
and its group dates are wrong for every promo bucket. A copyright year
(`2021 Copyright Date`) is a mark, not a date.

### 3.4 `number`, `total`, `baseSetSize`

`number` is as the card prints it; the loader's `PlainNumber` folds the
padding and the total. Two spellings that differ by zero padding alone are
one number and the card's own spelling wins over a padded catalog field
(Flesh and Blood's `HER0156` is `HER156`). A set size is published only
where the game prints one on the card (Pokemon, read back off the cards
that agree) or publishes one in data (Lorcana `cardCounts.base`, Riftbound
`collectorNumberMax`); Yu-Gi-Oh, Flesh and Blood, One Piece, Gundam and
Palworld publish none and none is derived from the highest number carried.
A pooled set whose cards print different totals (World Championship Decks,
the promo shelves) carries no size; 58 of Pokemon's 226 sets are like that.

A card's own `total` is the denominator its face prints, which is not the
set's size and is not always a number: Lorcana numbers a set's promos from
1 alongside the set's own cards and prints the run in place of the size,
`1/P1` beside `1/204`, so the number alone named two cards on 155 of its
(set, number) pairs. Lorcana's is read from `promoGrouping` where upstream
writes one and from the leading `N/D` of `fullIdentifier` otherwise; the
two never disagree on the 185 cards carrying both, and the grouping is
read first only because seven identifiers spell the promo number last.

## 4. Invariants every build enforces

`validate` re-reads the encoded output before anything is written,
peeling it through `emit.Unwrap` first: the output is the envelope now,
and a check that decoded the top level would validate a shape nothing
publishes. Refused
in every game: a card missing its identity fields; an id outside the id
shape; a number carrying whitespace; a duplicate id; more than
`emit.SharedIdentityLimit` (5) pairs of products wearing one identity
(`name|number|setCode|variant|…`, the exact tuple per game) - fewer are a
card TCGplayer listed twice, published and logged; a card in an unknown
set; a product emitting a
finish twice or emitting a set of finishes other than the catalog's skus
for it (**coverage, the zero-skip invariant**); a sealed entry missing its
identity or its set. Per game on top: a Pokemon build whose every priced
card has no image (one card without is logged, not refused),
a Pokemon set holding nothing, a Yu-Gi-Oh card with no product id (it
mints nothing), a Flesh and Blood minted entry wearing a priced entry's
`fabId` or its set, number and name, a Lorcana product claimed by two cards
or carried by none, a Riftbound printing naming a product the catalog does
not type as a card.

Then the **baseline guard** (`internal/baseline.Guard`): against the
previous baseline, refuse a card or sealed total that fell by more than
`-against-tolerance` (1%), a set that holds no card any more, or a set that
lost more than half; log every other per-set drop. Write `-baseline-fit`
only when the build holds at least as much as the baseline, so the baseline
only ever moves forward.

And **determinism**: unchanged inputs produce byte-identical output. Every
election, fold and tie-break sorts before it decides, and `OrderedFinishes`
breaks a shared `displayOrder` by name.

## 5. The builders

Common flags: `-tcg-catalog <file>` (required), `-o <file>` (stdout by
default), `-against <baseline.json>`, `-against-tolerance <fraction>`
(0.01), `-baseline-fit <path>`.

| game | category | upstream | upstream flags | what upstream adds |
|---|---|---|---|---|
| fleshandblood | 62 | the-fab-cube `flesh-and-blood-cards` (`card-flattened.json`, `set.json`) | `-fab-cards`, `-fab-sets` | `fabId`, pitch colour, artist; mints tokens and cards with no product, joined by dataset product id, then number (padding folded), then numberless same-name product |
| gundam | 86 | yzRobo `gcg-api` `data/cards.json` | `-gcg-cards` | the witness for names; mints the EX Base, EX Resource and Resource tokens; no image or text taken (licence unclear) |
| lorcana | 71 | LorcanaJSON `allCards.json` | `-lorcana` (required) | is the datastore; catalog adds product ids and the finishes sold |
| onepiece | 68 | punk-records `cards_by_id.json`, `packs.json`, the Cardmarket catalog | `-punk-cards`, `-punk-packs`, `-cardmarket-catalog` (required) | the witness for names; `bandaiId` where the pack agrees with the group; 58 pre-errata printings hand-carried from CardTrader blueprints, and the ones Cardmarket's pre-errata shelf sells beyond them minted from their product, read off the printing their number names |
| palworld | 91 | palworldtcg.gg `api/v1/cards` | `-palworld-cards` | numbers the one numberless product; English (`EBP01-001`) and Japanese (`BP01-001`) numbering reconciled to the printed form |
| pokemon | 3 | tcgdex GraphQL, pokemontcg.io sets, the Cardmarket catalog | `-tcgdex-sets`, `-tcgdex-cards`, `-pokemontcg-sets`, `-upstream-cache <dir>`, `-cardmarket-catalog` (required) | `tcgdexId`, symbols, set dates; mints tcgdex cards and sets the catalog lacks and the Cardmarket-only stamped promos (SEA, Professor Program) from the printing their number names |
| riftbound | 89 | the official card gallery (Next.js data URL resolved from the page) | `-gallery` | is the datastore; catalog decides finishes and adds the products the gallery lacks |
| yugioh | 2 | YGOPRODeck `cardsets.php`, `cardinfo.php` | `-ygoprodeck-sets`, `-ygoprodeck-cards` | release dates, passcodes (`konamiId`), the witness for names; no image (hotlinking forbidden) |

Every upstream flag accepts a path or a URL (`emit.Fetch`: a local file, or
an http(s) GET named `datastore-gen/1.0` and bounded at three minutes).
Pokemon's `-upstream-cache` keeps the last good response of each live API
and answers from it, dated in the log, when the API is unreachable.

Per-game notes an agent needs:

- **fleshandblood**: one entry per sku printing; pitch colour is read off
  the name (`(Red)`) before the dataset's field; `(Marvel)` stays in the
  name because the loader pins it (paired change outstanding).
- **gundam**: rarity is the variant axis (`C`, `C+`, `C++` at one number
  are three products); promotional reprints TCGplayer does not sell are
  hand-carried in `handCarriedPrintings` and stand down when a product
  appears.
- **lorcana**: a set code covers the set and its promo runs alike, because
  upstream files them under one and tells them apart by the denominator
  (§3.4); the puzzle inserts, lore cards and oversized components the
  catalog files with no `Number` carry no number rather than a `0`, which
  is only what an empty string parses to. The number is a string like
  every other game's, which it was not until the absence had to survive a
  consumer's own decoding: `validate` re-reads it as one, so a build
  publishing an integer again cannot get past the decode.
- **onepiece**: DON!! cards are all named `DON!! Card`, so the character on
  one is the mark, tested against the card names in the catalog with the
  epithet fold (`Rocks D. Xebec` is `Rocks.D.Xebec`); labels are cut at the
  pack seam (`Store Championship` + `Participation Pack`).
- **palworld**: one product per number; every published number wears the
  English prefix.
- **pokemon**: the biggest builder; names told apart per (group, number)
  bucket with a witness-free election; the promo shelves (`shelfNames`)
  read a set name as provenance; `variantOnlyQuals` is the hand list of
  marks; 22-character tokens split at a set or Pokemon head when the tail
  is a label the catalog writes on its own.
- **riftbound**: a group the gallery has no set for becomes its own set,
  including a group sold only sealed, so its sealed is not orphaned; a two-faced token product keeps both faces in
  its number; minted sets publish no size.
- **yugioh**: editions (`1st Edition`, `Unlimited`, `Limited`) are the
  finish; a rarity written into a name is the rarity; Speed Duel deck
  letters `(A)`…`(G)` stay in the name because the loader pins them
  (paired change outstanding); passcodes are checked against the name and
  withheld on contradiction.

## 6. Internal packages

**`internal/emit`** — `FinishSlug`, `FinishSuffix`, `PlainPrinting`,
`OrderedFinishes`, `PromoSlug`, `PlainQuotes`, `ImageURL`, `Fetch`,
`StringsOf`. The nine helpers every builder used to carry; one spelling
each. `PromoSlug` and `FinishSlug` are one function under two names.
`Envelope`, `Today` and the `SchemaVersion` constant spell §2's wrapper;
`Unwrap`, `UnwrapDocument` and `ErrUnknownSchema` read it back. One
spelling each, for all eight builders and for every reader of a built
file — the peel was eleven copies of a `json.RawMessage` peek before, and
a discriminator that has to be fixed in eleven places is fixed in none.

**`internal/baseline`** — `Counts`, `Count`, `Reader`, `Regression`,
`Options{Against, Tolerance, FitPath, Unit}`, `Guard`. Riftbound hands
`Guard` a reader of its own for its shape. `Count` peels through
`emit.Unwrap`, so a baseline that is not an envelope stops the build at
`-against`: "not a datastore envelope".

**`internal/vocabulary`** — `TokenLimit`, `Slug`, `Printing`, `Problems`,
`Check`, `SetNames`, `ReadDatastore`, `ErrNotDatastore`; `SetNames` and
`ReadDatastore` peel through `emit`, the envelope being plumbing rather
than an answer this package re-derives. `Slug` is kept
separate from `emit.PromoSlug` on purpose: a check that spelled its tokens
with the builders' own function would pass a builder whose spelling had
gone wrong. `TestPublishedVocabulary` reads `STORE_DIR/<game>.json` for the
eight games, skipping a file that is absent or is not an envelope: the raw
upstream, or a build from before the envelope.

**`internal/datastorediff`** — `Compare(before, after) (Change, error)`,
`Change.String()`: ids added and removed, fields published and dropped
(by name), values reworded, sets and sealed counted, with a leaf-by-leaf
fallback for the Riftbound shape. Both sides are peeled by
`emit.UnwrapDocument`, and a bare side is refused, so a range tagged again
now has to start after the envelope. Only meaningful when both files were
built from one catalog.

## 7. Workflows

**`ci.yml`** (push to master, pull requests): gofmt `-s`, vet, revive
1.13.0, staticcheck 2025.1.1, build, `go test -race`.

**`publish.yml`** (`37 12 * * *` UTC, and `workflow_dispatch` with `game`
and `rebaseline`): per game, one at a time,

1. `b2 file download b2://mtgban-datastore/<game>/tcgplayer-catalog.json.xz`;
2. the baseline, `<game>/<game>.baseline.json.xz`, else the published
   `<game>/<game>.json.xz` to seed one, else nothing;
3. Pokemon only: the upstream caches (`pokemon/tcgdex-sets.json.xz`,
   `tcgdex-cards.json.xz`, `pokemontcg-sets.json.xz`), plus a DNS pin for
   `api.tcgdex.net`; Pokemon and One Piece: the required
   `<game>/cardmarket_catalog.json.xz`;
4. `go run ./cmd/<game> -tcg-catalog tcgplayer-catalog.json [-against
   previous.json] [-lorcana …] [-upstream-cache .] [-cardmarket-catalog …]
   -baseline-fit baseline.fit -o <game>.json`;
5. `STORE_DIR="$PWD" go test ./internal/vocabulary -run TestPublishedVocabulary -count=1`;
6. `xz -9`, upload `<game>/<game>.json.xz`, and the same bytes as
   `<game>/<game>.baseline.json.xz` only when `baseline.fit` exists;
7. optionally tell `https://<game>.mtgban.com/api/load/datastore` to
   reload, signed with `BAN_SECRET`, for the games in `RELOAD_GAMES`.

Secrets: `B2_APPLICATION_KEY_ID_DATASTORE`, `B2_APPLICATION_KEY_DATASTORE`,
`BAN_SECRET`; variables: `DATASTORE_LORCANA`, `RELOAD_GAMES`.

**`tag-output-changes.yml`** (push to master, or a dispatched range): for
every commit, builds each touched game at the commit and at its parent on
one catalog and one set of upstream responses, runs `datastorediff`, and
tags the commit `<game>-vN` with the one-line difference when there is
one. A change under `internal/`, `go.mod` or `go.sum` counts as touching
every game.

## 8. The consumer's contract

go-mtgban loads each file through `mtgmatcher/<game>` and locates it by
environment variable: `FLESHANDBLOOD_PATH`, `GUNDAM_PATH`, `LORCANA_PATH`,
`ONEPIECE_PATH`, `PALWORLD_PATH`, `POKEMON_PATH`, `RIFTBOUND_PATH`,
`YUGIOH_PATH`, absolute paths. All eight loaders read §2's envelope and,
since go-mtgban's `710af946d`, nothing else. What it reads, and therefore
what a change here must keep true:

- **Identity** is `name` + `number` (+ `rarity` where the game sells one
  number at several rarities; + `total` in Pokemon), narrowed by the
  edition a storefront names. The loader folds the number's padding and
  total itself.
- **`promoTypes`** are declared as tags and used to tier candidates: a
  listing whose wording names a token reaches the printing wearing it; a
  listing naming none means the printing wearing none. A token's words come
  from the loader's `promoTypeLabels` table; a multi-word token with no row
  fails `internal/vocabulary.TestLoadersReadWhatIsPublished` there.
- **`watermark`** narrows when the wording names it, and an unmarked
  printing outranks a marked one for wording that names none (go-mtgban
  #543), which is what let print runs and energy kinds become marks.
- **`variant`** is indexed as the qualified name `name (variant)` for
  search and, in Yu-Gi-Oh, kept per printing so a plain listing means the
  product sold under the bare name.
- **`finish`** maps onto the loader's finish vocabulary (`Holofoil`,
  `Reverse Holofoil`, `1st Edition`…); a printing name it has not been
  taught passes through as itself.
- **`originalReleaseDate`** is preferred over the set's date wherever the
  loader dates a card; `language` refuses a listing in another language.
- **Riftbound** is read as the gallery payload and **Lorcana** as
  LorcanaJSON, both from under `data`, with Lorcana's `printings[]`
  deciding the uuids.

The measurement that ties the two sides together is go-mtgban's
`TestReplayCatalogNames` (`REPLAY_CATALOG=<catalog> <GAME>_PATH=<file>
REPLAY_OUT=<out>`): every card product name in the catalog, split into
name, number and qualifiers the way a storefront writes them, matched
against the datastore. Its output for the old and the new build, compared
by answer, is what every datastore PR reports. Its `REPLAY_SHELF_TOTALS=1`
mode composes each number over the shelf's usual total, the way a
storefront that derives `number/size` from its shelf writes it.

## 9. Glossary

- **catalog** — the TCGplayer category dump written by tcgdumper: groups
  (sets), products (cards and sealed) with `extendedData` (`Number`,
  `Rarity`, …), skus (printings by `printingId`).
- **printing** — one finish of one product, one entry, one uuid.
- **minted** — an entry or set built from upstream because the catalog has
  no product or group for it.
- **hand-carried** — an entry kept in a table in the builder because no
  source carries it (One Piece pre-errata prints, Gundam promotional
  reprints); each stands down when a source starts carrying it.
- **witness** — the upstream list's own name for a number, which decides
  whether a parenthetical is part of the card's name.
- **election** — the per-number vote that decides the same where no
  witness exists: a qualifier every product of the number carries is name.
- **fold** — collapsing two spellings of one label onto the one most
  listings use.
- **mark** — the `watermark` field: which copy of a number a printing is.
- **collision guard** — the pass that puts a dropped label back as the
  mark wherever losing it would leave two printings identical.
- **shelf** — a catalog group that pools cards from many sets (Deck
  Exclusives, Jumbo Cards, the promo buckets).
- **zero-skip invariant** — the emitted entries carry exactly the products
  the catalog types as a card and prices a sku for. One it prices nothing
  for yet has no printing to carry; it is logged as unpriced, and a builder
  with an upstream list mints the card from it in the meantime.
- **baseline** — the high-water mark a build is measured against;
  `<game>.baseline.json.xz` in the bucket.
- **replay** — go-mtgban's matching of every catalog product name against
  a datastore, the regression corpus for both repositories.
