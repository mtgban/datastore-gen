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

# shellcheck source=measure-lib.sh
source "$(dirname "$0")/measure-lib.sh"

# Which games a commit could possibly have changed.
games_touched() {
  git diff-tree --no-commit-id --name-only -r "$1" | games_in
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

echo "::group::fetching inputs"
# shellcheck disable=SC2086 # one game per word
fetch_inputs $NEEDED
echo "::endgroup::"

# Built from the tip, where the tool exists, rather than from whichever
# commit is being measured - most of them predate it.
go build -o "$WORK/datastorediff" ./cmd/datastorediff
git worktree add -q --detach "$BUILD" "$AFTER"

# The next number for a game, counted forward from the highest tag that
# already exists. It is held rather than re-read, so a run that assigns two
# numbers to one game gets two numbers - re-reading the tags would hand out
# the same one twice on a dry run, which creates none.
seed_number() {
  local g=$1 last
  # A game with no tag yet starts at 1; the empty grep must not end the run.
  last=$(git tag --list "$g-v*" | sed "s/^$g-v//" | grep -E '^[0-9]+$' | sort -n | tail -1 || true)
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
