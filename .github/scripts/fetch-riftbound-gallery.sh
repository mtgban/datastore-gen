#!/usr/bin/env bash
#
# Writes the Riftbound card gallery payload to the given path.
#
# The gallery is a Next.js page whose data lives behind a build id that
# changes whenever Riot redeploys, so the page has to be read first to
# learn where the data is. cmd/riftbound does this itself when given no
# -gallery file; this is the same two steps, done once, so that every
# build in a tagging run reads one payload instead of racing a redeploy.
set -euo pipefail
OUT=${1:?output path required}
python3 - "$OUT" <<'PY'
import re, sys, urllib.request
UA = {"User-Agent": "datastore-gen tagger"}
page = urllib.request.urlopen(urllib.request.Request(
    "https://riftbound.leagueoflegends.com/en-us/card-gallery/", headers=UA), timeout=60
).read().decode("utf-8", "ignore")
build = re.search(r'"buildId":"([^"]+)"', page)
if not build:
    sys.exit("no buildId in the card gallery page; the shape changed")
url = f"https://riftbound.leagueoflegends.com/_next/data/{build.group(1)}/en-us/card-gallery.json"
data = urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=120).read()
open(sys.argv[1], "wb").write(data)
print(f"riftbound gallery: {len(data)} bytes from build {build.group(1)}")
PY
