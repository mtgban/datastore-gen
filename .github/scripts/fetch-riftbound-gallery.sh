#!/usr/bin/env bash
#
# Writes the Riftbound card gallery payload to the given path.
#
# The gallery is a Next.js page whose data lives behind a build id that
# changes whenever Riot redeploys, so the page has to be read first to
# learn where the data is. cmd/riftbound does this itself when given no
# -gallery file; this is the same two steps, done once, so that every
# build in a tagging run reads one payload instead of racing a redeploy.
#
# A render listing a card row twice has dropped another card for it, so it
# is fetched again, as cmd/riftbound does, and nothing is written when no
# render comes back clean.
set -euo pipefail
OUT=${1:?output path required}
python3 - "$OUT" <<'PY'
import json, re, sys, time, urllib.request
UA = {"User-Agent": "datastore-gen tagger"}
# A render is cached for ten seconds, so a fetch that waits half a minute
# reads a new one. 14 of 20 read so on 2026-10-02 repeated a row, which
# twenty attempts leave at a 0.1% refusal.
ATTEMPTS, PAUSE = 20, 30

def repeated(data):
    """The ids of the card rows a render lists twice, identical in every field."""
    try:
        page = json.loads(data).get("pageProps", {}).get("page", {})
    except (ValueError, AttributeError):
        return []  # the build refuses a payload of another shape itself
    seen, out = set(), []
    for blade in page.get("blades", []):
        if blade.get("type") != "riftboundCardGallery":
            continue
        for row in blade.get("cards", {}).get("items", []):
            key = json.dumps(row, sort_keys=True)
            if key in seen:
                out.append(str(row.get("id")))
            seen.add(key)
    return out

page = urllib.request.urlopen(urllib.request.Request(
    "https://riftbound.leagueoflegends.com/en-us/card-gallery/", headers=UA), timeout=60
).read().decode("utf-8", "ignore")
build = re.search(r'"buildId":"([^"]+)"', page)
if not build:
    sys.exit("no buildId in the card gallery page; the shape changed")
url = f"https://riftbound.leagueoflegends.com/_next/data/{build.group(1)}/en-us/card-gallery.json"
for attempt in range(1, ATTEMPTS + 1):
    data = urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=120).read()
    rows = repeated(data)
    if not rows:
        break
    if attempt == ATTEMPTS:
        sys.exit(f"riftbound gallery: {ATTEMPTS} renders all listed a card row twice, the last {', '.join(rows)}")
    print(f"riftbound gallery: render {attempt} of {ATTEMPTS} lists {', '.join(rows)} twice; "
          f"fetching again in {PAUSE}s", file=sys.stderr)
    time.sleep(PAUSE)
open(sys.argv[1], "wb").write(data)
print(f"riftbound gallery: {len(data)} bytes from build {build.group(1)}")
PY
