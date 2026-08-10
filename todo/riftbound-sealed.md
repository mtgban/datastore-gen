# Sealed products in the Riftbound datastore

**Verdict: the matcher can carry Riftbound sealed products, and the design is
straightforward. It was not implemented, because the one input that decides
every classification rule — the category 89 catalog dump — is unreachable from
the environment this investigation ran in, and so is every substitute for it.
Building it here would have meant guessing at the shape of ~54 products nobody
in this session can see, and wiring those guesses into a nightly publish.**

What follows is the evidence, the full specification of what an implementation
must do, and the exact thing that unblocks it.

## 0. What could and could not be reached

Outbound network from this session is restricted by egress policy to GitHub and
the Go module proxy. Everything else answers `403` at the CONNECT:

| host | needed for | result |
|---|---|---|
| `tcgcsv.com` | reconstructing the category 89 product list | **403, blocked** |
| `f005.backblazeb2.com` | `b2://mtgban-datastore/riftbound/tcgplayer-catalog.json.xz` | **403, blocked** |
| `riftbound.leagueoflegends.com` | the gallery payload, to build a datastore | **403, blocked** |
| `mtgjson.com` | `ALLPRINTINGS5_PATH` | **403, blocked** |
| `api.tcgplayer.com` | live catalog (and no credentials are present anyway) | **403, blocked** |
| `*.blob.core.windows.net` | the `riftbound.json.xz` CI artifact | **403, blocked** |
| `github.com`, `api.github.com` (via MCP) | source, workflow logs | reachable |
| `proxy.golang.org` | module deps | reachable |

So the catalog dump could not be fetched, and it could not be reconstructed
from tcgcsv either. The datastore artifact that run 31317672070 uploaded is
also out of reach, so **no data-backed suite could be run**: not the Riftbound
golden, not the Magic golden, not Lorcana.

What *was* reachable turned out to carry real numbers: the publish workflow
logs every group it processes, and `go-tcgplayer`'s source (including
`cmd/tcgdumper`, which writes the dump) is in the module cache. Sections 2 and
3 are built on those.

## 1. What the matcher requires of a sealed product

A sealed entry is a `CardObject` with `Sealed: true`. Magic builds one in
`mtgmatcher/magic/mtgjson.go:475` (`generateSealedUUIDs`); that is the shape a
Riftbound entry has to fit.

### Fields that must be set

| field | why | available from the dump? |
|---|---|---|
| `Card.UUID` | key into `Backend.UUIDs`; must be stable across builds | yes — the TCGplayer product id |
| `Card.Name` | the only thing sealed is searched by (§1.2) | yes — `product.name` |
| `Card.SetCode` | buckets the entry in `SetSealedUUIDs` | yes — `group.abbreviation` |
| `CardObject.Edition` | display, and `GetSetByName` lookups | yes — `group.name` |
| `CardObject.Sealed` | the flag every consumer branches on | set by the loader |
| `Card.Identifiers["tcgplayerProductId"]` | `BuildSealedProductMap` (`api.go:1197`) keys the scrapers off it | yes — `product.productId` |
| `Card.Images` | `full`/`thumbnail` are contractual | yes — `product.imageUrl`, or derived as Magic does |
| `Card.Rarity` | Magic uses the literal `"product"` | invented, and that is fine |

### Backend indices that must be populated

The loader is responsible for all of these; nothing derives them:

- `AllSealedUUIDs` and `SetSealedUUIDs[code]` — `GetSealedUUIDs`,
  `GetSealedUUIDsInSet` (`api.go:25`, `api.go:42`). Both sorted.
- `AllSealed` / `AllCanonicalSealed` / `AllLowerSealed` — the normalized,
  canonical and lowercase sealed *name* lists behind `AllNames(variant, true)`.
- `Hashes[normalize(name)]` — `SearchSealedEquals` and `SearchSealedContains`
  (`api.go:183`, `api.go:251`) resolve a name to uuids **through `Hashes`**, the
  same map singles use. Sealed names must be added to it or sealed search
  silently returns nothing.
- `Sets[code].SealedProduct` — anything that walks sets rather than uuids reads
  this (`tcgplayer/sealed.go:145`, `sealedev/sealedev.go:415`).

### Code paths that depend on them

- **search** (`mtgban-website/search.go`): `/sealed` is a whole search mode.
  `search.go:1370` collects `GetSealedUUIDsInSet`, `search.go:1389` falls back
  from `SearchSealedEquals` to `SearchSealedContains`. Editions come from
  `SealedEditionsSorted`/`SealedEditionsList`.
- **upload** (`search.go:1267`): sealed listings are priced against a separate
  configured seller, chosen by `co.Sealed`.
- **arbit** (`arbit.go`): `NoSealed` / `SealedOnly` gate which scrapers feed
  which view; `arbit.go:636` matches them against `Info().SealedMode`.
- **newspaper / charts** (`api_chart.go:73`, `search.go:888`): `co.Sealed`
  selects the dataset.

None of these need anything Riftbound cannot supply. Two caveats, both real:

**`Match()` has no sealed exclusion.** It resolves through `Hashes`, which holds
sealed and single uuids together. Magic gets away with this because sealed
product names never collide with card names. A Riftbound builder has to
preserve that property; Riftbound has card names like `Dark Child - Starter`,
so the collision is not purely hypothetical.

**The website's title renderer will show a dangling colon.**
`mtgban-website/utils.go:215` builds a sealed card's subtitle from `co.Layout`
(MTGJSON's product *category*) and `co.Side` (its *subtype*). With both empty
the format string yields `"Origins -  Product : "`. Riftbound has no source for
either field, so this needs either invented values or a fix in the renderer.

## 2. What category 89 actually contains

This is where the investigation stops being able to answer concretely, and it
is worth being exact about what is and is not known.

The publish run [31317672070][run] (2026-08-09, the first build on the merged
catalog reader) logged, verbatim:

```
catalog: 10 groups, 1528 products, 1528 with a known finish
```

and then one line per group. Reconstructing the products that carry a `Number`
in `extendedData` — the ones `main.go` treats as singles:

| group | abbrev | numbered products |
|---|---|---|
| Origins | OGN | 352 (352 stamped, 0 unknown to the gallery) |
| Origins: Proving Grounds | OGS | 28 (24 + 4) |
| Spiritforged | SFD | 308 (288 + 20) |
| Unleashed | UNL | 308 (280 + 28) |
| Vendetta | VEN | 245 (227 + 18) |
| Riftbound Organized Play Promotional Cards | OPP | 216 |
| Riftbound Promotional Cards | PR | 14 |
| Riftbound Judge Promotional Cards | JDG | 3 |
| Secret Garden | SGN | *not examined* — skipped whole, "not in the gallery" |
| **(a tenth group, which logged nothing at all)** | ? | **0** |
| **total** | | **1474** |

`1528 − 1474 = 54`. **At most 54 products in category 89 lack a collector
number**, and that remainder is the entire candidate pool for "sealed".

Two things follow from the log that are worth stating outright:

- **A tenth group exists and produced no output.** Only nine group lines were
  logged. Reading `main.go`, the single silent path is a group where
  `isPromoGroup` is true (name contains `Promotional` or `Bundle`) *and* `added
  == 0`, i.e. a promo/bundle group in which **no product carries a number**.
  That is what a sealed-only group looks like. It is a strong inference, but it
  is an inference: the group's name, id and product count are all unlogged.
- **Every product has a finish.** "1528 with a known finish" means all 1528,
  the 54 included, have at least one SKU whose `printingId` maps to Normal or
  Foil. Whatever the sealed products are, they do have SKUs and do have
  printings. This is the one sealed-relevant fact the logs settle outright.

### How sealed is told apart from singles

`main.go` already draws the line, at the `continue` in the promo branch:

```go
number := product.extended("Number")
if number == "" {
    // Not a single (sealed, accessories, the odd unnumbered
    // promo): nothing to identify it by.
    continue
}
```

That comment names the problem exactly. "No `Number`" is not a sealed test — it
is a *not-a-numbered-single* test, and it catches sealed products, accessories
and unnumbered promos alike, plus everything in Secret Garden.

**The dump cannot narrow it further by product type.** `cmd/tcgdumper/main.go`
calls `ListAllProducts(..., AllProductTypes, true, page)` and appends the
resulting `[]tcgplayer.Product` into one flat array. `tcgplayer.Product`
(`tcgplayer.go:482`) has no product-type or category field. **The type each
product was fetched under is discarded before the dump is written.** So the
`"Booster Box"` / `"Sealed Products"` / `"Precon/Event Decks"` distinctions that
`ProductTypesSealed` is built from are not recoverable from the dump at all.

There is, however, a better discriminator in the dump than "no `Number`", and
it appears to have gone unnoticed: **SKU condition**. The dump carries the
category's `conditions` list, and `tcgplayer.SKU` carries `conditionId`.
TCGplayer files sealed product SKUs under the `UNOPENED` condition — go-mtgban
already relies on this in `tcgplayer/sealed.go:152` and `tcgplayer.go:318`, and
`SKUConditionMap` (`tcgplayer/api.go:126`) covers only conditions 1–5
(NM/SP/MP/HP/PO), so the singles scraper already skips unopened SKUs. A product
whose SKUs are all UNOPENED is sealed-or-accessory; one with graded conditions
is a single. That is a genuinely sound rule, and it is strictly better than the
number test — but it still does not separate a booster box from a playmat.

**None of this could be validated.** Without the dump there is no way to check
that category 89's conditions list even contains `UNOPENED` (Magic's does;
category 89's is unverified), what the 54 products are named, whether
accessories are in category 89 at all or filed under
`CategoryTCGplayerSupplies`, or how the 54 split across Secret Garden, the
tenth group, and the eight known groups.

## 3. Is that enough to build an entry the matcher accepts?

For the fields the matcher *requires*, yes — §1's table is fully satisfiable
from `productId`, `name`, `imageUrl` and `groupId`, all of which every product
in the dump has.

What has no source in Riftbound, and what it costs:

| field | Magic's source | Riftbound | consequence |
|---|---|---|---|
| `Contents` (what is in the box) | MTGJSON `sealedProduct.contents` | **nothing** | `GetProbabilitiesForSealed` finds nothing; `sealedev` computes no EV. Degrades quietly — both iterate `set.SealedProduct` contents and simply produce no rows. |
| `SourceProducts` | `fillinSealedContents` | **nothing** | "which product contains this card" is unavailable. Same quiet degradation. |
| `ReleaseDate` | MTGJSON, per product | **nothing** (groups carry `publishedOn`, which is the *group's* date) | sorting by release date falls back to set order. Acceptable. |
| `Category` → `Card.Layout` | MTGJSON | **nothing** | the dangling colon in `utils.go:215`. Cosmetic but visible. |
| `Subtype` → `Card.Side` | MTGJSON | **nothing** | same. |
| `CardCount` | MTGJSON | **nothing** | unused by the paths above. |

None of that blocks anything. Sealed EV was never going to work for Riftbound —
it needs a contents model no source publishes — and Lorcana, the other non-Magic
game, carries no sealed at all today, so nothing regresses by these being empty.

**The blocker is not a missing field. It is that the classification rule cannot
be written on evidence.** Emitting sealed means deciding, for each of ~54
products, whether it is a sealed product, an accessory, an unnumbered promo, or
a Secret Garden single — and every rule available (name heuristics, SKU
condition, group membership) needs the dump to validate. A rule that is wrong in
the accessory direction publishes playmats and sleeves as sealed products into a
nightly build, where they would be priced and arbitraged as though they were.

The `-min-cards` guard makes this concrete. The task asks for "an equivalent
sanity check for sealed", and a sanity check needs a calibrated floor. The only
honest floor derivable here is `≤ 54`, which is not a floor at all. Setting it
to 0 makes the guard decorative; setting it to a guess risks failing the nightly
publish, or worse, passing while emitting the wrong 54 things.

## 4. Does Riot's gallery carry anything sealed?

**No.** The gallery blade has exactly two item arrays, `sets.items` and
`cards.items` (`mtgmatcher/riftbound/riftbound.go`, `GalleryBlade`). A
`GalleryCard` is a single printing: `publicCode`, `collectorNumber`,
`cardType`, `rarity`, `domain`, `tags`, `cardImage`. There is no product,
bundle or box entity anywhere in the payload, and no field that could carry one.

The gallery is a *card* gallery in the literal sense, and Riot publishes no
product catalogue alongside it. So it is neither an alternative nor a
complementary source: **TCGplayer's category 89 is the only source for Riftbound
sealed**, which is why the dump is load-bearing rather than merely convenient.

## 5. What an implementation looks like, once the dump is in hand

Recorded so the next attempt does not re-derive it. This is a specification, not
a claim that it works.

**a. `riftbound-datastore`.** Classify unnumbered products by SKU condition
(§2), not by the absence of a `Number`; confirm against the dump that
`UNOPENED` exists in category 89's conditions and that the tenth group is what
it looks like. Emit survivors into a new array on the gallery blade —
`sealed.items`, parallel to `cards.items`, rather than smuggled into
`cards.items`, so an older loader ignores them instead of loading boxes as
cards. Carry `productId`, `name`, `groupId`→set code, `imageUrl`, and the
finishes the existing `finishesByProduct` already computes. Add `-min-sealed`,
calibrated against the real count. Keep `-min-cards` counting singles only.

**b. `mtgmatcher/riftbound`.** Read `sealed.items`; for each, build a
`CardObject` per §1 with `Sealed: true`; append to `AllSealedUUIDs`,
`SetSealedUUIDs`, the three sealed name lists and `Hashes`; populate
`Sets[code].SealedProduct`. Absent array ⇒ no sealed, exactly as the `finishes`
fallback does. Check for name collisions against `CanonicalNames` and refuse, or
the `Match()` hazard in §1 becomes live. Seed the golden with sealed cases.

**c. `go-mtgban` scrapers.** `TCGGame` and `TCGGameIndex` already carry a
`productTypes` field, already tag their logs when it is not
`ProductTypesSingles`, and already resolve the category through `SupportedGames`
— the parameterization is there and needs no Riftbound special-casing. Two
things do need changing: `Load()` hardcodes `TotalProducts(ctx, category,
[]string{"Cards"})` in both files and must pass `tcg.productTypes` (as
`generic.go:147` already does), and the match step must key off the product id
via `BuildSealedProductMap("tcgplayerProductId")` rather than calling
`mtgmatcher.Match`, which resolves singles. Magic's `TCGPlayerSealed` is not the
model to copy: it is driven by MTGJSON's per-product SKU map, which Riftbound
has no equivalent of.

## 6. What unblocks this

One file: a `tcgdumper` dump of category 89, the same
`b2://mtgban-datastore/riftbound/tcgplayer-catalog.json.xz` the publish workflow
already downloads. With it in the working tree, §2 becomes a twenty-line script
and §5 becomes an afternoon. Without it — or without egress to `tcgcsv.com` as a
partial substitute, which would give products but not the SKUs, and therefore
not the condition test that §2 turns on — every rule in §5(a) is a guess with a
nightly publish behind it.

[run]: https://github.com/mtgban/riftbound-datastore/actions/runs/31317672070
