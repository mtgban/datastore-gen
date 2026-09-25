# Yu-Gi-Oh's European first prints

`cmd/yugioh` mints the European first-print run of eleven early sets
whose catalog listing is the North American run only - LOB, MRD, MRL,
PSV, LON, SDY, SDK, DOR, PCY, TP1 and DL1 - plus three cards with no
North American printing to join to at all. YGOPRODeck's `cardsets.php`
names each run by a `<PREFIX>-E###` code; this build joins it to the
catalog's North American printing of the same card, labels it "European"
and adds it. Cardmarket prices 656 of the resulting rows as listings the
datastore previously had no printing to land on.

## What is minted, and what is not

A candidate is a `card_sets` code YGOPRODeck spells `<PREFIX>-E###`
(`europeanNumberRe`, `^([A-Z0-9]+)-E(\d{3})$`) whose prefix is a set this
build already publishes. It mints - one entry, at the set's own default
run (`europeanFinishOrder`: Unlimited, then 1st Edition, then Limited,
whichever the set's own priced entries carry first) - unless: a priced
TCGplayer product already carries that exact number (MRL-E104 through
E130 are TCGplayer products today, and stand down the same as any other
already-priced code); its joined North American sibling is not itself a
plain row (see "Why labelled, not plain" below); the set prices no card
at all, so there is no default run to read; an unjoined candidate's own
rarity is not one this build's own cards carry; or an unjoined name is
not itself a published one. Each of those is logged and the candidate is
skipped, not fatal - one bad upstream row must not take Yu-Gi-Oh off the
build.

Every prefix in one fetch of YGOPRODeck's `cardsets.php`/`cardinfo.php`:

| prefix | candidates | minted | already priced | not a plain sibling |
|---|---|---|---|---|
| LOB | 103 | 102 | 0 | 1 |
| MRD | 144 | 143 | 0 | 1 |
| MRL | 131 | 104 | 27 | 0 |
| PSV | 105 | 105 | 0 | 0 |
| LON | 105 | 105 | 0 | 0 |
| SDY | 46 | 46 | 0 | 0 |
| SDK | 46 | 46 | 0 | 0 |
| TP1 | 30 | 30 | 0 | 0 |
| DOR | 3 | 3 | 0 | 0 |
| PCY | 5 | 5 | 0 | 0 |
| DL1 | 2 | 2 | 0 | 0 |
| SDD | 3 | 0 | 3 | 0 |
| WC4 | 3 | 0 | 3 | 0 |
| **total** | **726** | **691** | **33** | **2** |

Two stand down as not-a-plain-sibling: LOB-E041 (Dark Hole, whose North
American LOB-052 carries the qualifier "Magic") and MRD-E008 (Harpie
Lady, whose North American MRD-008 is itself split "Original Artwork"
against "New Artwork", two variants and no plain row between them) -
minting beside either would leave every candidate a variant and no plain
row for an unlabelled listing to land on, so both stand down instead.
SDD and WC4 (`Stairway to the Destined Duel`, `World Championship 2004:
GBA Promo`) are video-game promo sets whose three E-numbered cards apiece
are already TCGplayer products; nothing mints for either.

**TP1 and PCY mint in full (35 rows) but reach no vendor today**: no
product in either set appears among the Cardmarket rows this change
lands (checked directly against the replay below). They mint anyway,
because the rule is the catalog and the North American sibling agreeing
a card exists, not which vendor happens to sell it this week -
MRL-E104..E130 is proof a vendor can start selling one later.

691 minted: 688 join their North American sibling by Konami's passcode,
falling back to a name join when the passcode does not resolve one (none
needed the fallback in this fetch). The other 3 - Time Wizard (DL1-E001),
Barrel Dragon (DL1-E002) and Beaver Warrior (MRL-E103) - mint unjoined,
from YGOPRODeck's own name, type, attribute and rarity, checked against
the names this build already publishes elsewhere: DL1 (`Duelist League
Series 1 participation cards`) prices two North American cards,
Thousand-Eyes Restrict (DL1-001) and Buster Blader (DL1-002) - DL1's own
default run is read from these two - but neither is Time Wizard or
Barrel Dragon, so both mint unjoined; Beaver Warrior's own North
American MRL printing does not exist even though the rest of MRL does.

**Rarity is the sibling's own, TCGplayer's catalog wording**, so the
"European" label stays the only difference between the two rows and a
listing that names a rarity can never leave the North American row for
the European one. The 3 unjoined cards mint YGOPRODeck's own
`set_rarity` instead, spelled through this build's own rarity table
(`Short Print` and `Super Short Print` folded to `Common`, the way the
North American side of these same sets already files every short print)
- none needed the fold this fetch.

Minting YGOPRODeck's own `set_rarity` instead, and only comparing it
against the sibling's, would disagree on 6 of 691 rows - DOR-E001/002/003
and PCY-E002/003/004, all `Prismatic Secret Rare` where the catalog
spells their sibling `Secret Rare`. That is not Europe against North
America: YGOPRODeck lists the North American DOR/PCY cards as `Prismatic
Secret Rare` too, and Cardmarket sells the European product as `Secret
Rare`. The six rows disagree because TCGplayer's own catalog is
inconsistent: it spells PCY-001 and PCY-005 `Prismatic Secret Rare` but
PCY-002/003/004 and DOR-001..003 `Secret Rare`, while YGOPRODeck spells
all eight `Prismatic Secret Rare`. With YGOPRODeck's rarity minted, a
listing that names `Prismatic Secret Rare` with an edition would move
from the North American row to the European one for those six - their
only landing-changing difference, and exactly what the label exists to
prevent. Reading the sibling's own rarity removes it.

Coverage is unaffected: 46513 of 46513 catalog products still carried, 0
skipped, 670 sets unchanged - a minted row carries no `tcgPlayerId`, so
it never enters the catalog-coverage count. `datastorediff` against the
same build without the mint: `ids +691/-0`, 0 sets changed, 0 sealed
changed. Two builds of the branch from identical inputs are
byte-identical.

## Why labelled, not plain

A plain minted `LON-E044` would collide with `LON-044` at every vendor
that sends a bare collector number or a rarity alone. The label is
`variant: "European"` plus `promoTypes: ["european"]`, appended after
`foldPromoTypes` runs so the fold cannot rewrite it back into a promo
type of its own. `mtgmatcher/yugioh`'s `tierByVariant` (go-mtgban,
unchanged by this work) files a labelled row under `variants` and
answers a plain listing from `base` alone - which is why the North
American sibling has to stay a plain row itself: a sibling that already
carries a qualifier (Dark Hole's LOB-052, Harpie Lady's MRD-008) leaves
`base` with nothing in it, so a second, labelled candidate would have
nothing to be chosen over. Where a passcode or name has more than one
row in a set - Summoned Skull's SDY-004 has both a plain row and a
"Sample Promo" row under one passcode - the plain row is the one
copied, regardless of which order `cards` lists them in.

Measured against the built datastores, with a vendor-shaped edition set
rather than none:

| input (name / edition / variation) | before this mint | after |
|---|---|---|
| Dark Magician / *LOB* / Ultra Rare, or no variation | lands on LOB-005 | unchanged |
| Dark Magician / *LOB* / "European" | lands on LOB-005 (word not recognized) | lands on LOB-E003 |
| Dark Magician / *LOB* / "LOB-E003" | unknown variant (no such card) | lands on LOB-E003 |
| Dark Hole / *LOB* / any plain wording | lands on LOB-052 | unchanged |
| Dark Hole / *LOB* / "LOB-E041" | unknown variant | unchanged (stands down) |
| Harpie Lady / Metal Raiders / "Original Artwork" | lands on MRD-008 | unchanged |
| Harpie Lady / Metal Raiders / "MRD-E008" | aliasing detected | unchanged (stands down) |
| Time Wizard / Metal Raiders / "MRD-E065" | lands on MRD-065 (word not recognized) | lands on MRD-E065 |
| Dark Magician / *Power of Chaos: Yugi the Destiny* / "Prismatic Secret Rare" | lands on PCY-004 | unchanged (PCY-E004 mints PCY-004's own rarity) |

(*LOB* is "The Legend of Blue Eyes White Dragon", the set's own published
name - the bare abbreviation is not a recognized edition and gives
`unknown variant` on either side of this change.)

A broader probe - every printing of every minted name, matched against
every rarity, number and edition shape a real vendor sends (98,743
inputs) - confirms it directly: comparing this build against the
unminted datastore, exactly one input moves, and it is not a landing
taken from a North American row. The input is Gaia the Fierce Knight
with no edition given and variation "006 Common"; it moves from an
aliasing error to `sdy-e006_unlimited`, and that landing is correct. The
European Starter Deck: Yugi drops Beaver Warrior from its own numbering
(the North American SDY-005), so every card after it shifts down one
slot: the North American Gaia is SDY-007 and SDY-006 is Dark Magician,
but Gaia's European sibling is SDY-E006. Zero inputs move from a North
American row to a European one.

## Known limitations

- **One run per set, not per card.** `europeanFinishOrder` picks a
  single default run from whichever the *set's* priced entries carry,
  not from the specific card's own North American sibling. PCY prices
  four Limited entries (001, 003, 004, 005) and one Unlimited (002);
  because `europeanFinishOrder` takes Unlimited as soon as any priced
  entry in the set carries it, all five PCY-E mints come out Unlimited,
  and four of them (PCY-E001, E003, E004, E005) differ from their own
  Limited sibling. Reading each sibling's own run would be more
  faithful; today one entry decides the run for the whole set.
- **One finish minted per code.** Where Cardmarket sells a card in more
  than one European run - a European 1st Edition alongside a European
  Unlimited - only the run `europeanFinishOrder` picked has a printing
  of its own; the other pools into that one row's price in Market.

## The identity guard

`validate` accepts a card with no `tcgPlayerId` only when its number is
shaped `<PREFIX>-E###` and it carries a `konamiId` - the exact shape
`mintEuropeanPrints` emits. Proved by mutating the real built output and
re-running `validate` on it: a minted row with `externalLinks` stripped
(no `konamiId`) is refused, `missing identity`; so is a priced row with
`tcgPlayerId` zeroed. The shared-identity check keys a minted row on its
own id, not product 0, so two European first prints of one set are never
folded into "the same card twice".

## A paired go-mtgban PR is required before this publishes

`mtgmatcher/yugioh`'s loader reads `id`/`name`/`finish` and falls back to
the id for its product key; it does not require `tcgPlayerId`, and a
minted Yu-Gi-Oh entry is a product of one printing standing for itself.
The Cardmarket resolver already asks `<set>-E<tail>` for a V.1 product
(`yugiohPrintNumber`, `otherPrintRun`) and lands it the moment that row
exists. Neither needs to change, and both are verified by the replays
below - but **publishing this datastore is not read-only for
go-mtgban**.

`cardmarket/yugiohprint_test.go`'s `TestYugiohIndexPrints`, on
go-mtgban's own `master`, pins the case "the European print gives the
bridged North American row up": `resolveProduct("Mystical Space Typhoon
(V.1 - Ultra Rare)" #047)` expects `errForeign`, because the unminted
datastore has no European row for Cardmarket's resolver to bridge the
V.1 product onto. Against this build it resolves to `mrl-e047_unlimited`
instead - a real card now, the change working as intended - and the
pinned test fails. CI's `test-yugioh` job runs
`./mtgmatcher/yugioh/... ./coolstuffinc/... ./cardmarket/...
./internal/vocabulary/...` against the *published*
`b2://mtgban-datastore/yugioh/yugioh.json.xz`, so publishing before that
case is updated turns both go-mtgban `master` and every open PR red.

The fix is mtgban/go-mtgban#873, which makes the case
hold on either datastore: it reads the European MRL-E047 row from the
backend and expects `errForeign` only where that row is absent. **That
PR merges before this datastore-gen PR publishes** - nothing else in
go-mtgban needs to change first: `go test ./mtgmatcher/yugioh/...
./coolstuffinc/... ./internal/vocabulary/...` already passes unmodified
against this build, `TestLoadersReadWhatIsPublished` included (`european`
is one word, needs no `promolabels` row).

Four production replays, before/after this build, with external market
data held constant:

- **Cardmarket** (`cardmarket` package, go-mtgban master `393cfc940`):
  `{foreign:23737 landed:45549 noprinting:71 twin:399}` before,
  `{foreign:23081 landed:46205 noprinting:70 twin:400}` after (the
  committed census walk, `docs/agents/cardmarket-census`, counts product
  842833 as landed rather than twin: 45550/46206 landed, 398/399 twin). Per
  product: 656 move `foreign` (a catalog we do not carry) to `landed`,
  each on the European-labelled row whose number matches the product's;
  1 moves `noprinting` to `twin` - Cardmarket's "Spell Ruler" prices
  Beaver Warrior and Serpent Night Dragon at the same plain number, a
  pre-existing same-number ambiguity the resolver already refuses
  rather than guess, refused both before and after. Nothing that landed
  before moves anywhere else after. Without the CardTrader bridge
  (`ZZ_NOBRIDGE=1`): the same 656 `foreign`->`landed`, plus 9
  `noprinting`->`twin` instead of 1 - still nothing that landed before
  moves.
- **CardTrader** (`cardtrader` package, 47,510 blueprints): 4 move, all
  refused to landed, all on the unjoined mints - Time Wizard (DL1-E001)
  and Barrel Dragon (DL1-E002) on the "Duelist League Promos Upperdeck"
  shelf, Beaver Warrior (MRL-E103) on the Magic Ruler and Spell Ruler
  shelves. None came from a North American row.
- **Cool Stuff Inc** (`coolstuffinc` package, 34,600 retail and 9,668
  buylist listings): 0 move.
- **TCGplayer catalog** (`internal/vocabulary`'s replay, 46,513
  products): 0 move.

Minting the sibling's own rarity rather than YGOPRODeck's `set_rarity`
keeps 6 rows (DOR-E001/002/003, PCY-E002/003/004) equal to their
sibling's TCGplayer wording (above). Of those, DOR-E001/002/003 are 3 of
the 656 Cardmarket landings (products 101864/102142/103517, Alpha/Beta/
Gamma The Magnet Warrior V.1 Secret Rare) - they land the same under
either rarity, since the Cardmarket resolver keys on set and number, not
the rarity string. The PCY three reach no vendor: all five Cardmarket PCY
products land on the plain North American row (PCY-001..005), not the
European one. All four replays are measured directly against go-mtgban
`master` `393cfc940` and this build.

Star City Games is not applicable: its `register.go` never carries
Yu-Gi-Oh.

## New dependency

`cardinfo.php` becomes load-bearing. Today, an outage costs only
passcodes; after this it costs these 691 rows too - 1.1% of the
catalog's ~61,000 card entries, past the baseline guard's 1% tolerance,
so a build run against a failing YGOPRODeck refuses to publish rather
than ship a file quietly missing them. That is the intended failure
mode.
