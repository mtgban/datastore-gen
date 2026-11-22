# Yu-Gi-Oh's European first prints

`cmd/yugioh` mints the European first-print run of eleven early sets
whose catalog listing is the North American run only - LOB, MRD, MRL,
PSV, LON, SDY, SDK, DOR, PCY, TP1 and DL1 - plus three cards with no
North American printing to join to at all. YGOPRODeck's `cardsets.php`
names each run by a `<PREFIX>-E###` code; this build joins it to the
catalog's North American printing of the same card, labels it "European"
and adds it, once per print run its North American sibling sells.
Cardmarket sells these prints as products of their own, which would
otherwise have no printing to land on.

## What is minted, and what is not

A candidate is a `card_sets` code YGOPRODeck spells `<PREFIX>-E###`
(`europeanNumberRe`, `^([A-Z0-9]+)-E(\d{3})$`) whose prefix is a set this
build already publishes. It mints one entry per run (Unlimited, 1st
Edition, Limited, in `europeanFinishOrder`) that its joined North
American sibling's plain rows sell; an unjoined candidate, or one whose
sibling sells none of the three, mints one entry at the set's default run,
the first of those the set's own priced entries carry - unless: a priced
TCGplayer product already carries that exact number (MRL-E104 through
E130 are TCGplayer products today), when it mints nothing and each plain
row of that product wears the European label instead; its joined North
American sibling is not itself a
plain row (see "Why labelled, not plain" below); the set prices no card
at all, so there is no default run to read; an unjoined candidate's own
rarity is not one this build's own cards carry; or an unjoined name is
not itself a published one. Each of those is logged and the candidate is
skipped, not fatal - one bad upstream row must not take Yu-Gi-Oh off the
build.

Every prefix in one fetch of YGOPRODeck's `cardsets.php`/`cardinfo.php`:

| prefix | candidates | minted | rows | already priced | not a plain sibling |
|---|---|---|---|---|---|
| LOB | 103 | 102 | 204 | 0 | 1 |
| MRD | 144 | 143 | 286 | 0 | 1 |
| MRL | 131 | 104 | 207 | 27 | 0 |
| PSV | 105 | 105 | 210 | 0 | 0 |
| LON | 105 | 105 | 210 | 0 | 0 |
| SDY | 46 | 46 | 92 | 0 | 0 |
| SDK | 46 | 46 | 92 | 0 | 0 |
| TP1 | 30 | 30 | 30 | 0 | 0 |
| DOR | 3 | 3 | 3 | 0 | 0 |
| PCY | 5 | 5 | 5 | 0 | 0 |
| DL1 | 2 | 2 | 2 | 0 | 0 |
| SDD | 3 | 0 | 0 | 3 | 0 |
| WC4 | 3 | 0 | 0 | 3 | 0 |
| **total** | **726** | **691** | **1341** | **33** | **2** |

A rows count twice the minted one is a set whose North American cards
sell in 1st Edition and Unlimited alike, so its European prints mint both
runs; TP1, DOR and PCY siblings sell Unlimited alone, and DL1's two
unjoined cards take the set's default run, Limited. MRL mints 207, not
208: Beaver Warrior (MRL-E103) is unjoined and mints Unlimited only.

Two stand down as not-a-plain-sibling: LOB-E041 (Dark Hole, whose North
American LOB-052 carries the qualifier "Magic") and MRD-E008 (Harpie
Lady, whose North American MRD-008 is itself split "Original Artwork"
against "New Artwork", two variants and no plain row between them) -
minting beside either would leave every candidate a variant and no plain
row for an unlabelled listing to land on, so both stand down instead.
SDD and WC4 (`Stairway to the Destined Duel`, `World Championship 2004:
GBA Promo`) are video-game promo sets whose three E-numbered cards apiece
are already TCGplayer products; nothing mints for either, and their rows
wear the label like MRL's: 60 priced rows in all (27 MRL products in two
runs, 3 SDD and 3 WC4 in one). On MRL-E130 the label is what tells
Serpent Night Dragon's European print from its North American MRL-103,
which a listing without the label aliased between.

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

Coverage is unaffected: a minted row carries no `tcgPlayerId`, so it
never enters the catalog-coverage count (47032 of 47032 catalog card
products carried, 0 skipped). Two builds from identical inputs are
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

- **An unjoined print mints one run.** Time Wizard, Barrel Dragon and
  Beaver Warrior have no sibling to read runs from, so each mints only
  the set's default run; a European copy in any other run has no
  printing of its own.
- **The sibling's runs are taken as the European print's.** card_sets
  names no run, so a European run the North American sibling never sold
  has no printing, and one it sold is minted whether or not Europe
  printed it.
- **TSC-E001..E003 stay unlabelled.** Exchange, Graceful Dice and Skull
  Dice (TCGplayer 236749, 236754, 236745) are priced European codes, but
  TCGplayer files them under VDP and no published set is coded TSC, so
  they are not candidates. No vendor landing depends on them today.

## The identity guard

`validate` accepts a card with no `tcgPlayerId` only when its number is
shaped `<PREFIX>-E###` and it carries a `konamiId` - the exact shape
`mintEuropeanPrints` emits. Proved by mutating the real built output and
re-running `validate` on it: a minted row with `externalLinks` stripped
(no `konamiId`) is refused, `missing identity`; so is a priced row with
`tcgPlayerId` zeroed. The shared-identity check keys a minted row on its
number, not product 0: the runs of one European first print pass as one
product's, the way a priced product's runs do, and two European first
prints of one set are never folded into "the same card twice".

## What go-mtgban needs before this publishes

`matcher/yugioh`'s loader groups entries into products by `productKey`:
a priced entry by its `tcgPlayerId`, a minted one by its number, so the
runs of one European first print load as one card with a uuid per run,
the way a priced product's runs do. A loader that keys a minted entry on
its own id reads the two runs as two cards of one identity: against
this build it moves 153 Card Game Corner listings onto the North
American row and refuses 8 as aliasing. So the go-mtgban change that
keys a minted entry on its number lands before this datastore publishes.
It changes nothing on a datastore minting one run per print.

`cardmarket`'s `TestYugiohIndexPrints` reads MRL-E047's default run and
1st Edition off the backend, so it holds on either shape. CI's
`test-yugioh` job runs go-mtgban against the *published*
`b2://mtgban-datastore/yugioh/yugioh.json.xz`, so a publish ahead of
those changes turns go-mtgban `master` red.

The Cardmarket resolver asks `<set>-E<tail>` for a V.1 product
(`yugiohPrintNumber`, `otherPrintRun`): it lands the default run and
names the minted 1st Edition as the product's first-edition column.

Production replays, before and after this build, on go-mtgban `master`
with the loader change, every input held still:

- **Cardmarket** (the census walk, `docs/agents/cardmarket-census`,
  70,223 products): 650 change, each landed to landed on the same
  European row, its first-edition column now the minted 1st Edition.
- **Card Game Corner** (13,747 Yu-Gi-Oh copies): 85 European 1st
  Edition copies, refused before so as not to price them as Unlimited,
  land on the minted 1st Edition rows; 0 lost, 0 moved.
- **CardTrader** (1,193,254 listings), **Cool Stuff Inc** buylist
  (9,543) and the **TCGplayer catalog** (61,090 product runs): 0 move.

Minting the sibling's own rarity rather than YGOPRODeck's `set_rarity`
keeps 6 rows (DOR-E001/002/003, PCY-E002/003/004) equal to their
sibling's TCGplayer wording (above); the Cardmarket resolver keys on set
and number, not the rarity string, so the choice moves no landing there.

Star City Games is not applicable: its `register.go` never carries
Yu-Gi-Oh.

## New dependency

`cardinfo.php` is load-bearing. An outage costs passcodes and these
1,341 rows - 2.1% of the catalog's ~62,400 card entries, past the
baseline guard's 1% tolerance, so a build run against a failing
YGOPRODeck refuses to publish rather than ship a file quietly missing
them. That is the intended failure mode.
