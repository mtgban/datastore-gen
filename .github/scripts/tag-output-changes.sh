#!/usr/bin/env bash
#
# Tags every commit in a range whose datastore output actually changed.
#
# The tags are {game}-vN, numbered per game in commit order. They are the
# repository's own answer to "when did this datastore last change", which
# its commit messages cannot give: roughly half the commits here leave the
# built output byte-identical, and a tag on each of those says nothing.
#
# The measurement only means anything with the inputs held still. Every
# build in one run reads the same catalog and the same upstream responses,
# fetched once below, so a card TCGplayer added overnight is present on
# both sides of every comparison and cancels out. What is left is the
# code's doing, which is what a tag should mark.
#
# Usage: tag-output-changes.sh <before-sha> <after-sha>
#   before-sha  the commit already tagged; the range is exclusive of it
#   after-sha   the tip to tag up to
#
# Set DRY_RUN=1 to print the tags without creating them, or PUSH_TAGS=0 to
# create them locally without sending any.
set -euo pipefail

BEFORE=${1:?before sha required}
AFTER=${2:?after sha required}
DRY_RUN=${DRY_RUN:-}
PUSH_TAGS=${PUSH_TAGS:-1}
WORK=$(mktemp -d)
# Builds happen in a worktree of their own, never by checking out in the
# tree this script is running from: checking out a commit that predates
# this file, or carries a different version of it, rewrites the script
# under the shell reading it.
BUILD="$WORK/build"
cleanup() {
  git worktree remove --force "$BUILD" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

GAMES="riftbound lorcana onepiece yugioh fleshandblood pokemon gundam palworld"

# The flags each builder needs beyond the catalog, and where this run pins
# them. A builder that has not grown a flag yet simply does not get it:
# every one is checked against the source at the commit being built, so one
# script spans the whole history rather than only its tip.
upstream_flags() {
  case "$1" in
    riftbound)     echo "gallery=$WORK/riftbound-gallery.json" ;;
    lorcana)       echo "lorcana=$WORK/lorcana-allcards.json" ;;
    onepiece)      echo "punk-cards=$WORK/punk-cards.json punk-packs=$WORK/punk-packs.json" ;;
    yugioh)        echo "ygoprodeck-sets=$WORK/ygo-sets.json ygoprodeck-cards=$WORK/ygo-cards.json" ;;
    fleshandblood) echo "fab-cards=$WORK/fab-cards.json fab-sets=$WORK/fab-sets.json" ;;
    pokemon)       echo "tcgdex-sets=$WORK/tcgdex-sets.json tcgdex-cards=$WORK/tcgdex-cards.json pokemontcg-sets=$WORK/pokemontcg-sets.json cardmarket-catalog=$WORK/pokemon-cardmarket.json" ;;
    gundam)        echo "gcg-cards=$WORK/gcg-cards.json" ;;
    palworld)      echo "palworld-cards=$WORK/palworld-cards.json" ;;
  esac
}

# Which games a commit could possibly have changed. A builder is standalone,
# so only its own directory and the module files can reach it; internal
# packages are checks with no importer and cannot.
games_touched() {
  local sha=$1 touched=""
  local files
  files=$(git diff-tree --no-commit-id --name-only -r "$sha")
  if grep -qE '^go\.(mod|sum)$' <<<"$files"; then
    echo "$GAMES"; return
  fi
  for g in $GAMES; do
    grep -q "^cmd/$g/" <<<"$files" && touched="$touched $g"
  done
  echo "$touched"
}

COMMITS=$(git rev-list --reverse --no-merges "$BEFORE..$AFTER")
if [ -z "$COMMITS" ]; then
  echo "nothing to tag: $BEFORE..$AFTER is empty"
  exit 0
fi

NEEDED=""
for sha in $COMMITS; do
  for g in $(games_touched "$sha"); do
    grep -qw "$g" <<<"$NEEDED" || NEEDED="$NEEDED $g"
  done
done
if [ -z "${NEEDED// /}" ]; then
  echo "nothing to tag: no commit in the range touches a builder"
  exit 0
fi
echo "games this range could have changed:$NEEDED"

# One catalog and one set of upstream responses for the whole run. Only the
# games in the range are fetched, which is what keeps a push touching one
# builder from pulling a quarter of a gigabyte it will not read.
echo "::group::fetching inputs"
for g in $NEEDED; do
  b2 file download --no-progress "b2://mtgban-datastore/$g/tcgplayer-catalog.json.xz" "$WORK/$g-cat.json.xz" >/dev/null
  xz -d "$WORK/$g-cat.json.xz"
done
fetch() { curl -sSL --retry 3 --retry-all-errors --max-time 180 -A "datastore-gen tagger" "$1" -o "$2"; }
for g in $NEEDED; do
  case $g in
    riftbound)     "$(dirname "$0")/fetch-riftbound-gallery.sh" "$WORK/riftbound-gallery.json" ;;
    lorcana)       b2 file download --no-progress b2://mtgban-datastore/lorcana/allCards.json "$WORK/lorcana-allcards.json" >/dev/null ;;
    onepiece)      fetch https://raw.githubusercontent.com/buhbbl/punk-records/main/english/index/cards_by_id.json "$WORK/punk-cards.json"
                   fetch https://raw.githubusercontent.com/buhbbl/punk-records/main/english/packs.json "$WORK/punk-packs.json" ;;
    yugioh)        fetch https://db.ygoprodeck.com/api/v7/cardsets.php "$WORK/ygo-sets.json"
                   fetch https://db.ygoprodeck.com/api/v7/cardinfo.php "$WORK/ygo-cards.json" ;;
    fleshandblood) fetch https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/card-flattened.json "$WORK/fab-cards.json"
                   fetch https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/set.json "$WORK/fab-sets.json" ;;
    gundam)        fetch https://raw.githubusercontent.com/yzRobo/gcg-api/main/data/cards.json "$WORK/gcg-cards.json" ;;
    palworld)      "$(dirname "$0")/fetch-palworld-cards.sh" "$WORK/palworld-cards.json" ;;
    pokemon)       b2 file download --no-progress b2://mtgban-datastore/pokemon/tcgdex-sets.json.xz "$WORK/tcgdex-sets.json.xz" >/dev/null && xz -d "$WORK/tcgdex-sets.json.xz"
                   b2 file download --no-progress b2://mtgban-datastore/pokemon/tcgdex-cards.json.xz "$WORK/tcgdex-cards.json.xz" >/dev/null && xz -d "$WORK/tcgdex-cards.json.xz"
                   b2 file download --no-progress b2://mtgban-datastore/pokemon/cardmarket_catalog.json.xz "$WORK/pokemon-cardmarket.json.xz" >/dev/null && xz -d "$WORK/pokemon-cardmarket.json.xz"
                   fetch "https://api.pokemontcg.io/v2/sets?pageSize=250" "$WORK/pokemontcg-sets.json" ;;
  esac
done
echo "::endgroup::"

# Built from the tip, where the tool exists, rather than from whichever
# commit is being measured - most of them predate it.
go build -o "$WORK/datastorediff" ./cmd/datastorediff
git worktree add -q --detach "$BUILD" "$AFTER"

# Build one game at one commit, into $WORK/<game>-<sha>.json. The flags are
# read off that commit's own source, so a commit predating a flag is built
# without it rather than failing on one it does not define.
build_at() {
  local g=$1 sha=$2 out="$WORK/$1-${2:0:12}.json"
  [ -f "$out" ] && { echo "$out"; return 0; }
  git -C "$BUILD" checkout -q --detach "$sha" || return 1
  [ -d "$BUILD/cmd/$g" ] || return 1
  local args=""
  for kv in $(upstream_flags "$g"); do
    local name=${kv%%=*} path=${kv#*=}
    grep -q "\"$name\"" "$BUILD/cmd/$g/main.go" 2>/dev/null && args="$args -$name $path"
  done
  # shellcheck disable=SC2086
  ( cd "$BUILD" && go run "./cmd/$g" -tcg-catalog "$WORK/$g-cat.json" $args -o "$out" ) >/dev/null 2>&1 || return 1
  echo "$out"
}

# The next number for a game, counted forward from the highest tag that
# already exists. It is held rather than re-read, so a run that assigns two
# numbers to one game gets two numbers - re-reading the tags would hand out
# the same one twice on a dry run, which creates none.
seed_number() {
  local g=$1 last
  last=$(git tag --list "$g-v*" | sed "s/^$g-v//" | grep -E '^[0-9]+$' | sort -n | tail -1)
  eval "NEXT_$g=$(( ${last:-0} + 1 ))"
}
# Sets TAKEN rather than printing, because a command substitution runs in a
# subshell and the increment would be lost with it - which is how a dry run
# came to offer one number to two commits.
take_number() {
  local g=$1 n
  eval "n=\$NEXT_$g"
  TAKEN=$n
  eval "NEXT_$g=$(( n + 1 ))"
}

for g in $NEEDED; do
  seed_number "$g"
done

TAGGED=0
CREATED=""
for sha in $COMMITS; do
  subject=$(git log -1 --format=%s "$sha")
  for g in $(games_touched "$sha"); do
    # Already tagged: a re-run must not renumber what a previous run did.
    if git tag --points-at "$sha" | grep -q "^$g-v"; then
      echo "  ${sha:0:9} $g already tagged $(git tag --points-at "$sha" | grep "^$g-v")"
      continue
    fi
    # Measured against this commit's own parent, never against whatever the
    # walk built last. They are the same thing on a linear push and are not
    # on a merge: a pull request's first commit has an older master for a
    # parent, and comparing it to today's tip would put everything master
    # did since the branch point into that one commit's tag. A root commit
    # has no parent and cannot be measured at all.
    parent=$(git rev-parse --verify --quiet "$sha^1") || {
      echo "  ${sha:0:9} $g has no parent to measure against; skipped"; continue; }
    prev=$(build_at "$g" "$parent") || {
      echo "  ${sha:0:9} $g does not build at its parent; skipped"; continue; }
    out=$(build_at "$g" "$sha") || { echo "  ${sha:0:9} $g does not build; skipped"; continue; }
    change=$("$WORK/datastorediff" "$prev" "$out")
    [ "$change" = "no change" ] && continue
    take_number "$g"
    tag="$g-v$TAKEN"
    echo "  ${sha:0:9} $tag  $change"
    if [ -z "$DRY_RUN" ]; then
      CREATED="$CREATED $tag"
      git tag -a "$tag" "$sha" -m "$subject

Built output changed: $change.
Measured by building cmd/$g at this commit and at the commit before it
against one catalog and one set of upstream responses, and comparing the
two datastores."
    fi
    TAGGED=$((TAGGED+1))
  done
done

echo "$TAGGED tags"
[ -n "$DRY_RUN" ] && exit 0
[ "$PUSH_TAGS" = "1" ] || { echo "not pushing:$CREATED"; exit 0; }
# Only the tags this run made. "git push --tags" sends every tag the local
# repository holds, which on a machine carrying unpushed work is a much
# larger thing than this run decided to do. --no-force so a name already
# taken on the remote fails loudly instead of moving under someone.
for t in $CREATED; do
  git push --no-force origin "refs/tags/$t"
done
exit 0
