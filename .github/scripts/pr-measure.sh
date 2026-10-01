#!/usr/bin/env bash
#
# Measures what a change does to the datastores it can reach, the way every
# pull request here reports it: each touched game built before and after on
# one catalog and one set of upstream responses, then
#
#   - what datastorediff says moved,
#   - whether the after side builds byte-identical twice,
#   - how many log lines changed,
#   - whether the vocabulary check passes on the after side,
#   - what the baseline guard says of the after side against the published
#     baseline, and
#   - when GOMTGBAN names a go-mtgban checkout, how the catalog replay's
#     answers moved: error to resolved, resolved to error, another printing.
#
# The report is markdown, written to $GITHUB_STEP_SUMMARY when it is set and
# to stdout either way; the diffs and logs behind it are left in $OUT.
#
# Usage: pr-measure.sh <before-sha> <after-sha>
#   before-sha  the commit the change is measured from: a pull request's
#               merge base, so master's own commits since are not counted
#   after-sha   the commit measured
set -euo pipefail

# Full ids: the builds are named by an id's first twelve characters, and a
# revision like "<sha>^" would name both ends alike.
BEFORE=$(git rev-parse --verify "${1:?before sha required}^{commit}")
AFTER=$(git rev-parse --verify "${2:?after sha required}^{commit}")
OUT=${OUT:-$PWD/measure-out}
WORK=$(mktemp -d)
BUILD="$WORK/build"
cleanup() {
  git worktree remove --force "$BUILD" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT
mkdir -p "$OUT"

# shellcheck source=measure-lib.sh
source "$(dirname "$0")/measure-lib.sh"

GAMES_TOUCHED=$(git diff --name-only "$BEFORE" "$AFTER" | games_in)
if [ -z "${GAMES_TOUCHED// /}" ]; then
  echo "No builder is reached by this change." | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
  exit 0
fi

echo "::group::fetching inputs"
# shellcheck disable=SC2086 # one game per word
fetch_inputs $GAMES_TOUCHED
for g in $GAMES_TOUCHED; do
  b2 file download --no-progress "b2://mtgban-datastore/$g/$g.baseline.json.xz" "$WORK/$g-baseline.json.xz" >/dev/null &&
    xz -d "$WORK/$g-baseline.json.xz" || echo "no baseline for $g"
done
echo "::endgroup::"

go build -o "$WORK/datastorediff" ./cmd/datastorediff
git worktree add -q --detach "$BUILD" "$AFTER"
VOCABULARY=$(pwd)

# The log lines one build wrote and the other did not, timestamps aside.
log_diff() {
  diff <(sed -E 's/^[0-9/]+ [0-9:]+ //' "$1" | sort) <(sed -E 's/^[0-9/]+ [0-9:]+ //' "$2" | sort) >"$3" || true
  grep -c '^[<>]' "$3" || true
}

# The catalog replay of one datastore, through go-mtgban at $GOMTGBAN.
replay() {
  local g=$1 store=$2 out=$3 var
  var="$(tr '[:lower:]' '[:upper:]' <<<"$g")_PATH"
  ( cd "$GOMTGBAN" && env -i HOME="$HOME" PATH="$PATH" GOPATH="$(go env GOPATH)" \
      GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" \
      REPLAY_CATALOG="$WORK/$g-cat.json" "$var=$store" REPLAY_OUT="$out" \
      go test ./internal/vocabulary/ -run TestReplayCatalogNames -count=1 >/dev/null 2>&1 )
}

# How the replay's answers moved, compared by name and answer rather than by
# line, since the matcher rewrites the edition column.
replay_moves() {
  python3 - "$1" "$2" "$3" <<'PY'
import collections, sys
def answers(path):
    out = collections.defaultdict(list)
    for line in open(path):
        name, _, answer = line.rstrip("\n").split("\t", 2)
        out[name].append(answer)
    return out
before, after = answers(sys.argv[1]), answers(sys.argv[2])
err = lambda a: a.startswith("ERR")
moves = collections.Counter()
rows = []
for name in sorted(set(before) | set(after)):
    for old, new in zip(sorted(before.get(name, [])), sorted(after.get(name, []))):
        if old == new:
            continue
        kind = ("error to resolved" if err(old) and not err(new) else
                "resolved to error" if err(new) and not err(old) else "another printing")
        moves[kind] += 1
        rows.append(f"{kind}\t{name}\t{old}\t{new}")
open(sys.argv[3], "w").write("\n".join(rows) + ("\n" if rows else ""))
total = sum(len(v) for v in after.values())
print(f"{total} names, " + (", ".join(f"{n} {k}" for k, n in moves.most_common()) if moves else "no answer moves"))
PY
}

ROWS=""
for g in $GAMES_TOUCHED; do
  echo "::group::$g"
  before=$(build_at "$g" "$BEFORE") || { ROWS+="| $g | does not build at ${BEFORE:0:9} | | | | | |"$'\n'; echo "::endgroup::"; continue; }
  after=$(build_at "$g" "$AFTER") || { ROWS+="| $g | **does not build** | | | | | |"$'\n'; cp "$WORK/$g-${AFTER:0:12}.json.log" "$OUT/" 2>/dev/null || true; echo "::endgroup::"; continue; }
  again=$(build_at "$g" "$AFTER" again) || again=""
  cp "$before.log" "$OUT/$g-before.log"; cp "$after.log" "$OUT/$g-after.log"

  moved=$("$WORK/datastorediff" "$before" "$after")
  determinism="no"; [ -n "$again" ] && cmp -s "$after" "$again" && determinism="yes"
  logs=$(log_diff "$before.log" "$after.log" "$OUT/$g-log.diff")

  mkdir -p "$WORK/store-$g" && cp "$after" "$WORK/store-$g/$g.json"
  vocabulary="passes"
  ( cd "$VOCABULARY" && STORE_DIR="$WORK/store-$g" go test ./internal/vocabulary -run TestPublishedVocabulary -count=1 >"$OUT/$g-vocabulary.txt" 2>&1 ) || vocabulary="**fails**"

  guard="no baseline"
  if [ -f "$WORK/$g-baseline.json" ]; then
    if build_at "$g" "$AFTER" guard -against "$WORK/$g-baseline.json" >/dev/null; then
      guard="publishes"
    else
      guard="**refuses**: $(grep -h 'refusing to publish' "$WORK/$g-${AFTER:0:12}-guard.json.log" | sed -E 's/^[0-9/]+ [0-9:]+ //; s/^against: refusing to publish: //' | head -1)"
    fi
  fi

  answers="not run"
  if [ -n "${GOMTGBAN:-}" ]; then
    replay "$g" "$before" "$WORK/$g-replay-before.txt" && replay "$g" "$after" "$WORK/$g-replay-after.txt" &&
      answers=$(replay_moves "$WORK/$g-replay-before.txt" "$WORK/$g-replay-after.txt" "$OUT/$g-replay.diff") ||
      answers="**replay failed**"
  fi

  ROWS+="| $g | $moved | $determinism | $logs | $vocabulary | $guard | $answers |"$'\n'
  echo "::endgroup::"
done

{
  echo "### Datastores, ${BEFORE:0:9} → ${AFTER:0:9}"
  echo
  echo "Each game built at both commits on one catalog and one set of upstream responses."
  echo
  echo "| game | datastorediff | rebuilds identical | log lines changed | vocabulary | baseline guard | go-mtgban replay |"
  echo "|---|---|---|---|---|---|---|"
  printf '%s' "$ROWS"
  echo
  echo "The diffs and logs behind each number are in the run's \`measure\` artifact."
} | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}" > "$OUT/summary.md"
cat "$OUT/summary.md"
