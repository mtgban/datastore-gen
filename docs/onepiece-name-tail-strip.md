# One Piece: the number-tail strip and the four names it fixes

`cmd/onepiece/main.go`'s `decompose()` peels a card's collector number off
the end of its raw TCGplayer name ("Yamato - OP16-098" -> "Yamato"). It
matched only the literal `" - "+num`, so a name whose catalog spacing
differed kept the tail and stood apart from its own card's other
printings. Two shapes reach the published datastore:

- no space after the dash: `"Jewelry Bonney -PRB02-004"`
- a doubled space before the number: `"Trafalgar Law -  P-088 (Reprint)"`
  (the trailing `"(Reprint)"` is irrelevant here; it is peeled by the
  parenthetical-qualifier pass regardless of whether the tail stripped)

`stripNumberTail` replaces the literal match with a scan that requires at
least one space before the dash (so a name's own hyphen, e.g.
"Neo-Marine", is never touched) and tolerates any amount of space on
either side of it.

`rawNames` gains one entry, `671375: "Monkey.D.Luffy"`: the catalog
writes that product's number as `OP14-34`, one digit short of its real
`OP14-034`, so no text match reaches it either way.

## Datastore diff

Built before and after from the same TCGplayer and Cardmarket catalog
inputs. The build reports 7,420 card entries over 7,232 products (a
product with both a Normal and a Foil sku publishes two card entries).
Exactly 4 of the 7,420 rows change, each a `name` field only:

| id | before | after |
|---|---|---|
| `op14-034_671375_foil` | `Monkey.D.Luffy - OP14-34` | `Monkey.D.Luffy` |
| `p-088_656220` | `Trafalgar Law - P-088` | `Trafalgar Law` |
| `p-088_656226_foil` | `Trafalgar Law - P-088` | `Trafalgar Law` |
| `prb02-004_653263_foil` | `Jewelry Bonney -PRB02-004` | `Jewelry Bonney` |

No id is added or removed, no other field on any row moves, and `sets`,
`sealed` and `game` are byte-identical. No published name still contains
`" - "`. The two DON!! `" // "` names (`don_456320`, `don_549343`) are
untouched; splitting them is out of scope here (see "Left out," below).

## go-mtgban verification (read-only, no go-mtgban commit)

Every run below is against the production code path, not a hand-built
`Match()` call; the three replays below the suite compare the before-
and after-datastore.

**`mtgmatcher/onepiece` suite**: full pass, including
`TestOnepieceDashTailNames` (it targets a different, bare-number-only
shape than the four renamed rows) and the golden replay corpus.

**CoolStuff Inc** (real recorded HTTP tape, through the production
`csi.processSearch`/`parseBL`): retail landed 4062 -> 4063, refused
57 -> 56; buylist landed 1107 -> 1108, refused 25 -> 24. Four listings
move:

- `429954` "Monkey.D.Luffy (034)": same landing (671375); cosmetic name
  cleanup only.
- `423993` "Jewelry Bonney - 004" ("PRB02-004 Smiling"): moves from
  wrongly landing on `653264` (Alternate Art) to correctly landing on
  `653263`, the plain printing the wording names. Sibling listing
  `423994` "(Alternate Art)" still lands on `653264`, unaffected.
- `424038` "Trafalgar Law - P-088" ("White Border"): stays refused
  (aliasing) both before and after, but not as the same pair relabeled
  — they are two different printings. Before, the candidates are the
  two PRB-02 printings, `656220` (nonfoil, Reprint) and `656226`
  (foil, Pirate Foil); "White Border" doesn't say which finish. After,
  the plain name reaches two OP-PR printings instead (P-088 has three
  published OP-PR foils; the aliasing message names two of them), and
  both are foil, so the ambiguity that survives is between two OP-PR
  printings, not a finish call. No price moves either way.
- `425305` "Trafalgar Law - P-088 (Pirate Foil)": moves from refused
  (aliasing) to landed on `656226_foil`, tagged `piratefoil`, in both
  retail and buylist.

**Game Nerdz** (captured buylist feed, through the production
`resolveProduct`, `buylist_products` mode): landed 5607 -> 5609,
refused 19 -> 17, error unchanged at 6. Four products move, all
corrections:

- `l4TUTEtSfB` "Jewelry Bonney (PRB02-004) ... Foil": moves off the
  wrongly-landed `653264` (Alternate Art) onto the plain `653263` —
  the same fix as CSI's `423993` above.
- `nWaHj9iIoc` "Trafalgar Law -  P-088 (Pirate Foil) ...": moves from
  refused ("unknown variant") to landed on `656226` (foil, Pirate
  Foil).
- `qpTAlg4lc9` "Trafalgar Law -  P-088 (Reprint) ...": moves from
  refused ("unknown variant") to landed on `656220` (nonfoil,
  Reprint).
- `o6fABinmCE` "Monkey.D.Luffy - OP14-34 ...": same landing (671375);
  cosmetic name cleanup only.

**Cardmarket Index** (cached id-map/catalog/productlist/priceguide,
through production `resolveMapped`/`claimByID`/`giveWay`/`twinsAmong`):
`error` 158 -> 157, `noprinting` 150 -> 151; `landed` (6963) and `twin`
(210) unchanged. Two rows move:

- `904376` "Trafalgar Law (P-088) (V.1)": `error`/aliasing ->
  `noprinting`.
- `904377` "(V.2)": stays `error`/aliasing; its candidate list grows
  from 2 (the PRB-02 pair) to 4, adding the two pre-existing OP-PR
  P-088 foils (`646737`, `710212`).

  Both rows are non-pricing refusals before and after, so this is a
  refusal-detail change with zero pricing impact. The DON!! Cardmarket
  listing (`768199`) is untouched, as expected.

No row moves from a correct landing to a wrong one, or from landed to
refused, in any of the three replays.

## Left out

- **The DON!! `" // "` split** (`don_456320`, `don_549343`): go-mtgban's
  `gamenerdz` `preprocessOnePieceDon` still sends the literal
  `"DON!! Card // Green Compass"` as the whole name. Renaming it here
  alone would turn that Game Nerdz listing into an unknown-name refusal
  until go-mtgban's scraper is updated too — a coordinated rollout, not
  this change.
- **A build-time refusal on a published `" // "` name**: this repo's
  `AGENTS.md` says a build must never let one product stop a whole game.

## Follow-up after this datastore republishes

Comment-only, no behavior change: `mtgmatcher/onepiece/rules.go`'s
`dashTailRe` comment and `TestOnepieceDashTailNames`'s doc comment both
cite `"Monkey.D.Luffy - OP14-34"` as a still-malformed example; update
both once the fixed name ships.
