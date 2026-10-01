# shellcheck shell=bash
#
# What every script that builds a datastore to measure it needs: the games,
# the upstream flags each builder takes and where one run pins them, which
# games a set of changed files reaches, the inputs fetched once for a run,
# and a build of one game at one commit.
#
# Sourced, not run. The caller sets WORK (a scratch directory holding the
# inputs and the builds) and BUILD (a git worktree the builds check commits
# out in, never the tree the caller runs from).

GAMES="riftbound lorcana onepiece yugioh fleshandblood pokemon gundam palworld"
MEASURE_LIB_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# The flags each builder needs beyond the catalog, and where this run pins
# them. A builder that has not grown a flag yet simply does not get it:
# every one is checked against the source at the commit being built, so one
# script spans the whole history rather than only its tip.
upstream_flags() {
  case "$1" in
    riftbound)     echo "gallery=$WORK/riftbound-gallery.json" ;;
    lorcana)       echo "lorcana=$WORK/lorcana-allcards.json cardmarket-catalog=$WORK/lorcana-cardmarket.json" ;;
    onepiece)      echo "punk-cards=$WORK/punk-cards.json punk-packs=$WORK/punk-packs.json cardmarket-catalog=$WORK/onepiece-cardmarket.json" ;;
    yugioh)        echo "ygoprodeck-sets=$WORK/ygo-sets.json ygoprodeck-cards=$WORK/ygo-cards.json" ;;
    fleshandblood) echo "fab-cards=$WORK/fab-cards.json fab-sets=$WORK/fab-sets.json" ;;
    pokemon)       echo "tcgdex-sets=$WORK/tcgdex-sets.json tcgdex-cards=$WORK/tcgdex-cards.json pokemontcg-sets=$WORK/pokemontcg-sets.json cardmarket-catalog=$WORK/pokemon-cardmarket.json" ;;
    gundam)        echo "gcg-cards=$WORK/gcg-cards.json" ;;
    palworld)      echo "palworld-cards=$WORK/palworld-cards.json" ;;
  esac
}

# Which games a list of changed files, one per line on stdin, could have
# changed. A builder reaches its own directory, the module files, and the
# internal packages it imports; internal/ is treated as reaching every
# builder rather than reading each one's imports, since a package none of
# them imports yet costs a run of builds that all say "no change".
games_in() {
  local files touched=""
  files=$(cat)
  if grep -qE '^(go\.(mod|sum)|internal/)' <<<"$files"; then
    echo "$GAMES"; return
  fi
  for g in $GAMES; do
    grep -q "^cmd/$g/" <<<"$files" && touched="$touched $g"
  done
  echo "$touched"
}

# One catalog and one set of upstream responses for the whole run. Only the
# games named are fetched, which is what keeps a change touching one builder
# from pulling a quarter of a gigabyte it will not read.
fetch_inputs() {
  local g
  fetch() { curl -sSL --retry 3 --retry-all-errors --max-time 180 -A "datastore-gen tagger" "$1" -o "$2"; }
  bucket() { b2 file download --no-progress "b2://mtgban-datastore/$1" "$2.xz" >/dev/null && xz -d "$2.xz"; }
  for g in "$@"; do
    bucket "$g/tcgplayer-catalog.json.xz" "$WORK/$g-cat.json"
    case $g in
      riftbound)     "$MEASURE_LIB_DIR/fetch-riftbound-gallery.sh" "$WORK/riftbound-gallery.json" ;;
      lorcana)       fetch https://lorcanajson.org/files/current/en/allCards.json "$WORK/lorcana-allcards.json"
                     bucket lorcana/cardmarket_catalog.json.xz "$WORK/lorcana-cardmarket.json" ;;
      onepiece)      fetch https://raw.githubusercontent.com/buhbbl/punk-records/main/english/index/cards_by_id.json "$WORK/punk-cards.json"
                     fetch https://raw.githubusercontent.com/buhbbl/punk-records/main/english/packs.json "$WORK/punk-packs.json"
                     bucket onepiece/cardmarket_catalog.json.xz "$WORK/onepiece-cardmarket.json" ;;
      yugioh)        fetch https://db.ygoprodeck.com/api/v7/cardsets.php "$WORK/ygo-sets.json"
                     fetch https://db.ygoprodeck.com/api/v7/cardinfo.php "$WORK/ygo-cards.json" ;;
      fleshandblood) fetch https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/card-flattened.json "$WORK/fab-cards.json"
                     fetch https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/develop/json/english/set.json "$WORK/fab-sets.json" ;;
      gundam)        fetch https://raw.githubusercontent.com/yzRobo/gcg-api/main/data/cards.json "$WORK/gcg-cards.json" ;;
      palworld)      "$MEASURE_LIB_DIR/fetch-palworld-cards.sh" "$WORK/palworld-cards.json" ;;
      pokemon)       bucket pokemon/tcgdex-sets.json.xz "$WORK/tcgdex-sets.json"
                     bucket pokemon/tcgdex-cards.json.xz "$WORK/tcgdex-cards.json"
                     bucket pokemon/cardmarket_catalog.json.xz "$WORK/pokemon-cardmarket.json"
                     fetch "https://api.pokemontcg.io/v2/sets?pageSize=250" "$WORK/pokemontcg-sets.json" ;;
    esac
  done
}

# Build one game at one commit into $WORK/<game>-<sha>[-<suffix>].json, with
# the build's log beside it as .log, and print the path. The flags are read
# off that commit's own source, so a commit predating a flag is built
# without it rather than failing on one it does not define. A suffix names a
# second build of the same commit - again, or with the extra flags after it.
build_at() {
  local g=$1 sha=$2 suffix=${3:-}
  shift 3 2>/dev/null || shift $#
  local out="$WORK/$g-${sha:0:12}${suffix:+-$suffix}.json"
  [ -f "$out" ] && { echo "$out"; return 0; }
  git -C "$BUILD" checkout -q --detach "$sha" || return 1
  # A commit before the builder existed built nothing: measured against an
  # empty datastore, so the commit that adds a builder is measured for what
  # it published rather than skipped as unbuildable at its parent.
  [ -d "$BUILD/cmd/$g" ] || { printf '{"cards":[],"sets":{},"sealed":[]}' > "$out"; echo "$out"; return 0; }
  local args="" kv name path
  for kv in $(upstream_flags "$g"); do
    name=${kv%%=*} path=${kv#*=}
    grep -q "\"$name\"" "$BUILD/cmd/$g/main.go" 2>/dev/null && args="$args -$name $path"
  done
  # shellcheck disable=SC2086
  ( cd "$BUILD" && go run "./cmd/$g" -tcg-catalog "$WORK/$g-cat.json" $args "$@" -o "$out" ) >"$out.log" 2>&1 || return 1
  echo "$out"
}
