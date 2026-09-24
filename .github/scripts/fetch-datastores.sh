#!/usr/bin/env bash
#
# Refresh the local datastores consumed by go-mtgban's tests and hooks. The
# published object is compressed in B2, while the local checkout expects one
# uncompressed JSON file per game under output/.
set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
readonly ROOT
readonly OUTPUT_DIR=${OUTPUT_DIR:-"$ROOT/output"}
readonly B2_BUCKET=mtgban-datastore
readonly ALL_GAMES=(
  riftbound
  lorcana
  onepiece
  yugioh
  fleshandblood
  pokemon
  gundam
  palworld
)

usage() {
  echo "usage: $0 [game ...]" >&2
  echo "games: ${ALL_GAMES[*]}" >&2
}

is_game() {
  local candidate=$1 game
  for game in "${ALL_GAMES[@]}"; do
    [[ "$candidate" == "$game" ]] && return 0
  done
  return 1
}

decompress() {
  local archive=$1
  if command -v xz >/dev/null 2>&1; then
    xz -d "$archive"
    return
  fi
  python3 - "$archive" <<'PY'
import lzma
import pathlib
import sys

archive = pathlib.Path(sys.argv[1])
archive.with_suffix("").write_bytes(lzma.decompress(archive.read_bytes()))
archive.unlink()
PY
}

games=("${ALL_GAMES[@]}")
if (($# > 0)); then
  games=("$@")
fi

for game in "${games[@]}"; do
  if ! is_game "$game"; then
    echo "unknown game: $game" >&2
    usage
    exit 2
  fi
done

mkdir -p "$OUTPUT_DIR"
workdir=$(mktemp -d "$OUTPUT_DIR/.fetch-datastores.XXXXXX")
trap 'rm -rf "$workdir"' EXIT

for game in "${games[@]}"; do
  archive="$workdir/$game.json.xz"
  json="$workdir/$game.json"

  echo "downloading $game"
  b2 file download --no-progress \
    "b2://$B2_BUCKET/$game/$game.json.xz" \
    "$archive"
  decompress "$archive"
  mv -f "$json" "$OUTPUT_DIR/$game.json"
done
