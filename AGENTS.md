# AGENTS.md

Guidance for AI coding agents working on **datastore-gen**, the builders
behind the mtgban game datastores. Each command under `cmd/` ingests the
TCGplayer catalog dump for one game, joins it against a public card dataset,
and emits one JSON datastore that go-mtgban and the mtgban website consume.
Read `SPECIFICATIONS.md` for the document format, the per-game sources and
the invariants; read `README.md` for the human-facing overview.

## The one rule that matters

**A datastore is the catalog's products and the upstream's cards, joined,
and nothing is ever dropped silently.** Every builder re-reads its own
encoded output before writing and refuses to publish when the products the
entries carry are not exactly the products the catalog prices (the
zero-skip invariant), when a set code is claimed twice, when more than a
handful of product pairs wear one identity, or when the build shrank past
the baseline.

**One product never stops a game.** A refusal takes every card in the game
off the nightly, so it has to be about all of them: an upstream that will
not read, coverage, the baseline, a pile of collisions, every image gone. A
product or upstream row that no rule expected is carried by the general
rule, or set aside when there is nothing to carry, and logged either way.
So a card filed with no number goes on its bare product id, a second card
under a taken upstream id goes on an id of its own, a card listed twice is
published as the pair it is, and a product priced for nothing yet waits
until it is. Before this was the rule, riftbound stopped for three nights
in 2026-09 on one gallery row, and onepiece on one trophy card.

Two corollaries shape every change:

- **Ids are opaque and generative.** An id is spelled from the collector
  number, the TCGplayer product id and the printing name, by rule, every
  build. There is no id map to maintain and there must never be one; a rare
  respelling when a finish is renamed is accepted rather than carried. A
  consumer reads the fields, never the shape of an id.
- **The catalog decides the finishes.** A printing TCGplayer prices a sku
  for exists; one it does not is not invented from upstream. Upstream
  supplies annotation (a printing id, a clean image, a release date) and the
  cards the catalog has no product for, which are *minted* beside the priced
  entries and name no product because none exists.

## Layout

```
cmd/<game>/main.go     one builder per game: fleshandblood, gundam, lorcana,
                       onepiece, palworld, pokemon, riftbound, yugioh
cmd/datastorediff      reads two built datastores, prints what the second did
                       to the first (used by the tagging workflow)
internal/emit          what every builder spells the same way: finish suffix,
                       plain printing, finish order, promo slug, quote
                       normalisation, image link, Fetch, StringsOf
internal/baseline      the guard that refuses a build which lost too much
internal/vocabulary    the promo-type rules, checked against a built file
internal/datastorediff the comparison behind cmd/datastorediff
.github/workflows      ci.yml, publish.yml, tag-output-changes.yml
```

Each `cmd/<game>/main.go` is one large file on purpose. The builders are
standalone: no dependency on go-mtgban, no external module beyond the
catalog reader (`github.com/mtgban/go-tcgplayer`) and the Cardmarket reader
(`github.com/mtgban/go-cardmarket`). Shared code lives only under
`internal/`, which is inside this module and drags nothing. Do not add a
dependency to move a helper.

A rule keeps its evidence, or the next reader deletes what they cannot see
the point of. Keep it short: a comment is two or three lines carrying the
one number that justifies the rule ("111 of the 116 that name a set we
carry"). Evidence that runs longer goes in a file under `docs/`, committed
with the change, and the comment points at it.

## Build, test, format

```sh
gofmt -s -l .                                   # must print nothing
go vet ./...
go run github.com/mgechev/revive@v1.13.0 -set_exit_status -config .revive.toml ./...
go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
go build ./...
go test -race ./...
```

That is what `ci.yml` runs on every push and pull request; run all of it
before committing. Unit tests live in `internal/*` and in the builders that
have them (`cmd/pokemon`, `cmd/fleshandblood`, `cmd/lorcana`,
`cmd/riftbound`); most builder behaviour is verified by building, below.

## Inputs, and where they come from

Every builder takes the TCGplayer catalog dump with `-tcg-catalog` and
writes with `-o`. The dump is written nightly by tcgdumper
(`github.com/mtgban/go-tcgplayer/cmd/tcgdumper`) and sits in the bucket
beside the published datastore:

```sh
b2 file download b2://mtgban-datastore/<game>/tcgplayer-catalog.json.xz <game>.json.xz
xz -d <game>.json.xz
```

Upstream sources default to their public URLs and every one can be pinned
to a saved file, which is how a comparison is held still: `-fab-cards` and
`-fab-sets`, `-gcg-cards`, `-lorcana` (required, no default),
`-punk-cards` and `-punk-packs`, `-palworld-cards`, `-tcgdex-sets`,
`-tcgdex-cards`, `-pokemontcg-sets` and `-cardmarket-catalog` (required),
`-gallery`, `-ygoprodeck-sets` and `-ygoprodeck-cards`. Full list per game
in `SPECIFICATIONS.md`.

**Never measure on a stale catalog.** The catalog changes nightly, and a
finding measured against last week's dump is confidently wrong ("34 Palworld
colours lost" was the catalog, not the code). Re-pull the catalog before the
first measurement of a task, and hold one catalog and one set of upstream
files still across every build you compare.

## How to verify a change

Every behaviour change to a builder is measured, not argued. The method,
which every PR body in the history reports:

1. **Build master and the branch on identical inputs.** Build the master
   binary once, build the branch, run both against the same catalog and the
   same saved upstream files, and build the branch twice: the two branch
   outputs must be byte-identical (determinism).
2. **Diff the outputs.** `go run ./cmd/datastorediff old.json new.json`
   says what moved in the terms a reader asks about: ids added or removed,
   fields published or dropped, values reworded, sets and sealed counts. Then
   look at the rows behind each number; a count is not an explanation.
3. **Diff the logs** (strip the timestamps first). The builders log every
   election, fold and refusal.
4. **Run the vocabulary check on the new file**, with an absolute path,
   because `go test` runs in the package directory:

   ```sh
   mkdir -p /abs/dir && cp new.json /abs/dir/<game>.json
   STORE_DIR=/abs/dir go test ./internal/vocabulary -run TestPublishedVocabulary -count=1
   ```
5. **Run go-mtgban against the new file.** A datastore change is tested by
   the loader that reads it, from a checkout of go-mtgban at `origin/master`
   (fetch first; a stale loader measures code nobody runs):

   ```sh
   # every package that reads the game, the loader word table included
   <GAME>_PATH=/abs/new.json go test ./mtgmatcher/... ./internal/... -count=1
   # the catalog replay: every product name, before and after
   REPLAY_CATALOG=/abs/catalog.json <GAME>_PATH=/abs/old.json REPLAY_OUT=/abs/before.txt \
     go test ./internal/vocabulary/ -run TestReplayCatalogNames -count=1
   REPLAY_CATALOG=/abs/catalog.json <GAME>_PATH=/abs/new.json REPLAY_OUT=/abs/after.txt \
     go test ./internal/vocabulary/ -run TestReplayCatalogNames -count=1
   ```

   Set only the one game's path for a replay (the harness replays every
   game whose path is set, and the last one overwrites the output file), and
   classify the diff by what each answer did: error to resolved, resolved to
   error, resolved to a different printing. Compare by answer, not by line:
   the edition column is rewritten by the matcher, so lines move. A change
   that turns a resolved answer into an error or another printing is a
   regression unless the old answer was wrong, and the PR says which.
6. **Report the numbers in the PR body.** The commit message carries the
   one that proves the change; a long classification or table goes in a
   file under `docs/`. Only numbers measured on the final code against the
   current master count; one measured before a rebase is re-measured.
7. **A change to what a build refuses is measured by breaking one
   product.** Put the one anomaly into a real catalog or upstream file:
   strip a product's number, its skus or its image, list it a second time
   under a new id, repeat an upstream row. Build master and the branch on
   it. Master refusing and the branch publishing is the finding. The
   unmodified catalog must still come out byte-identical to master's, and
   go-mtgban has to read the new output.

The catalog replay is what finds things; the unit tests pin what it found.
Every real defect in the 2026-09 review came out of the replay and none out
of a suite.

## The conventions every builder follows

These are settled across all eight games; a change that breaks one in one
game is wrong even if it improves that game.

**Names.** The card's name is what the game's own list calls it. Which
parenthetical is part of the name is decided by an upstream *witness*
(gcg-api for Gundam, YGOPRODeck for Yu-Gi-Oh, punk-records for One Piece)
where one exists, and otherwise by election within a collector number (a
qualifier every product of the number carries is name; the rest is
variant). A qualifier that restates the number or the rarity is dropped.

**`promoTypes` is a query vocabulary of promotions.** Each token is a
lowercase slug of letters and digits, at most 22 characters, and names what
promoted a printing: a stamp, an event, a shelf. Four things are never a
token: a rarity, a finish, a number (bare, a deck place, a span, a
restatement), and a subject (what the card pictures: a Pokemon, a One Piece
character on a DON!! card, a Gundam form). One label has one spelling,
folded to the wording most listings arrive in; where the singles disagree
the sealed side is the authority. `internal/vocabulary` refuses a file that
breaks these and the publish workflow runs it.

**A fact a fold removes is published as a field.** The set a promo reprints,
the year in "World Championships 2013", the language in "Japanese Exclusive"
and the ink of a Duelist League printing come off the token and go to
`watermark`, `originalReleaseDate`, `language` and `color`. `variant`
always keeps the catalog's own wording, joined, so nothing is lost.

**The mark (`watermark`) says which copy of a number a printing is**, not
what promoted it: the player whose deck a card came in, the set a shelf
reprinted it from, the character on a DON!! card, the finish where two
products share a number apart from it. Every drop goes through the
collision guard, which puts a dropped label back as the mark wherever
losing it would leave two printings identical.

**Numbers are as written on the card.** `number` is the catalog's own
spelling with a printed total split into `total` where the game prints one;
a set size is published only where the game prints or publishes one, never
derived from the highest number carried. Two spellings of one number that
differ by zero padding alone are one number, and the card's spelling wins.

**Cards and the sealed that holds them, nothing else.** No accessories.
Do not write a filter for it; the sealed side is whatever the category dump
does not type as a card, and a name-pattern filter flags real product.

**Hand tables report their own staleness.** A row carried by hand (a
subject, a hand-carried printing, a respelling) logs when nothing uses it
any more, and stands down the day a source carries the same fact.

## The loader is the other half of every rule

go-mtgban reads what this repository publishes, and its matcher has
semantics a datastore change must respect. The ones that have bitten:

- **A token demotes; a mark narrows.** A plain listing means the printing
  with no token where a sibling has one, and, since go-mtgban#543, the
  unmarked one where a sibling is marked. Before #543 a label could move
  from token to mark only where no plain sibling shared the number.
- **A qualified name is searchable.** The loader indexes `name (variant)`
  beside the name, and for Yu-Gi-Oh keeps the catalog's qualifier per
  printing: a listing that says nothing means the product sold under the
  bare name.
- **A multi-word token wants a row in the loader's word table**
  (`mtgmatcher/<game>/promolabels.go`), or a reader sees it run together.
  A token published before its row is reported by go-mtgban's
  `TestLoadersReadWhatIsPublished` as a label that is due, without
  failing. Since go-mtgban#779, a row for a token no datastore declares
  fails nothing either, so the row can land before the publish or after
  it. List it in the PR body either way.
- **Names carrying their own parenthetical** ("Delta Plus (Waverider
  Mode)") need the loader to rejoin what a storefront splits; Gundam got
  that in go-mtgban#540. Check the replay before publishing a name shape a
  game has not published before.
- **A total no printing prints** ("Blastoise 2/25" on a 25-card shelf) is
  the loader's to forgive (go-mtgban#542), not the datastore's to publish.
- **A change to the document's shape is a paired change.** The envelope
  parses as nothing in a consumer that does not unwrap it, so the loader
  side lands and deploys before the writer does (go-mtgban#604), and reads
  both shapes for as long as a file built before the change might still
  arrive. Anything that moves what `data` holds owes the same order, and
  `meta.version` is what tells a reader it has to care.
- **What the datastore can publish is part of the loader's contract.**
  go-mtgban's onepiece loader refused a whole file over one card with no
  number, until go-mtgban#771. Gundam's and palworld's reachability tests
  failed on a card listed twice, until go-mtgban#780. Before a build
  publishes a shape its game has not published before, run go-mtgban
  against a build that carries it, and land the loader side first.

The go-mtgban pre-push hook sources `.env` (the worktree's own, else the
main checkout's) and runs the whole `go test ./...` against the datastores
it names, which are the ones in this repository's `output/`. go-mtgban
reads only the envelope since `710af946d`, so a bare file left there fails
every loader, and with it every push, until `output/` is refreshed (below).
A session that has to push before then can give its worktree an `.env`
that sources the main one and points the `*_PATH` variables at datastores
downloaded from the bucket. `.env` is gitignored.

## Publishing

`publish.yml` runs daily (`37 12 * * *` UTC) and on demand for one game or
all, one game at a time: it downloads the catalog and the game's upstream
caches from `b2://mtgban-datastore/<game>/`, builds with `-against` the
previous baseline and `-baseline-fit`, runs `TestPublishedVocabulary` on
the built file, and uploads `<game>/<game>.json.xz`; the build becomes
`<game>.baseline.json.xz` only when it held at least as much as the
baseline it was measured against. `rebaseline` on a dispatch ignores the
baseline for that run. `tag-output-changes.yml` then builds every touched
game at the commit and its parent on one catalog and tags the commit
`<game>-vN` with what changed, so the history can be read by output.

Publishing is not the end of a change: go-mtgban's tests and hook read the
datastores from datastore-gen's `output/` directory on the developer's
machine, so after a publish, refresh `output/<game>.json` (and
`output/catalogs/`) from the bucket. `.github/scripts/fetch-datastores.sh
[game ...]` does the former, into this checkout's `output/` unless
`OUTPUT_DIR` names another.

## Git

- **Work in your own worktree**, never in the developer's main checkout:
  `git worktree add <dir> origin/master`, and remove it when done. Worktrees
  share the branch list, so delete only the branches you created, by name.
  Never sweep "merged" branches; that deleted someone else's on 2026-09-10.
- **Sync before measuring.** `git fetch origin` in every repo the task
  touches, and rebase the branch onto `origin/master` before trusting a
  test or a number.
- **One PR per concern, every PR based on `master`, never stacked** on
  another open PR. Rebase onto master before opening and after siblings
  merge; the builders' import blocks are where PRs collide.
- **Commit as the repository's identity** (the repo's git config). The
  title is about 50 characters; the body, a few lines of why and the
  number that proves it, points at a `docs/` file for anything longer.
  Never name a person in a comment, a commit or a doc; attribute a
  decision to its date.
- **Never push without being asked for that push.** Build, test and commit
  locally, then stop and say what is ready. A push agreed earlier in the
  same task covers that branch; a new branch, repo or force-push needs its
  own yes. Never `--no-verify` around a hook without being told to.

## Working with subagents

Delegate the reading to subagents and keep the reasoning: inventories of a
builder's flags and entry keys, sweeps across the eight `main.go` files,
"which rows carry token X", the replay classification. Use a Sonnet-class
model (`model: sonnet`) for those subagents; they are search and summary
work and do not need the largest model. Give each a precise question, a
root path, and the instruction not to modify files, and ask for the exact
quotes and paths rather than paraphrase. Keep one rule for yourself: a
subagent's report is evidence to check against the file, not a finding.

## Gotchas

- `STORE_DIR` for the vocabulary check must be absolute; `go test` runs the
  binary in `internal/vocabulary`, and `STORE_DIR=.` reads nothing (that
  shipped once and broke every publish until #65).
- Every datastore is a `{"meta":…,"data":…}` envelope (SPECIFICATIONS
  §2), and since 2026-09-24 every reader here refuses anything else: a
  bare file is an upstream payload or a build from before the envelope.
  Read one through `emit.Unwrap` (or `emit.UnwrapDocument`) and never by
  hand: an envelope is `meta` *and* `data`, and a peel that keys on `data`
  alone silently re-roots into any document that happens to publish a
  field by that name.
- Riftbound's datastore is the gallery payload itself, under the envelope's
  `data`: the cards are at `data.pageProps.page.blades[].cards.items[]`,
  not under a `cards` key at any level. A reader that stops at the top
  level reports the game empty and clean. `internal/vocabulary` and
  `internal/datastorediff` know the path.
- Lorcana's card ids are integers (LorcanaJSON's own; a minted product is
  the negated product id), and each card carries a `printings[]` array with
  a uuid per finish (`1951`, `1951_foil`, `m-714954_holofoil`).
- In zsh an unquoted variable is not word-split: `env $FLAGS cmd` passes
  one argument. Spell the flags out or use `${=FLAGS}`. The symptom is a
  replay whose every answer is "unknown card name" because another game's
  backend answered.
- TCGplayer's catalog contradicts itself in places: a Number field padded
  to four digits under a name that says three, a name whose number
  disagrees with the field, a rarity in the name the field does not carry.
  The builders resolve each by a stated rule with its evidence in a short
  comment; add to those rather than special-casing a product id.
- `go-mtgban` has its own `AGENTS.md`; read it before touching the loader
  side of a paired change.
