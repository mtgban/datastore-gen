#!/usr/bin/env bash
#
# Writes the whole Palworld card list to the given path, as one array.
#
# palworldtcg.gg pages its answers, so the list has to be walked. cmd/
# palworld follows the paging itself when given no -palworld-cards file and
# reads a bare array back from one; this writes that array, so every build
# in a tagging run reads one list rather than paging the API again per
# build.
set -euo pipefail
OUT=${1:?output path required}
python3 - "$OUT" <<'PY'
import json, sys, urllib.request
UA = {"User-Agent": "datastore-gen tagger"}
rows, url = [], "https://palworldtcg.gg/api/v1/cards?limit=100"
# A page count nothing sane reaches, so a server answering with a cycle of
# next links stops this rather than running forever.
for _ in range(200):
    if not url:
        break
    page = json.load(urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=60))
    rows.extend(page["data"])
    url = page.get("meta", {}).get("next")
json.dump(rows, open(sys.argv[1], "w"))
print(f"palworld: {len(rows)} cards")
PY
