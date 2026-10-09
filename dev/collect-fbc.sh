#!/usr/bin/env bash
# For each release on the overview: what the release page shows for FBC (GET only).
set -euo pipefail
B=${B:-http://127.0.0.1:8088/api/v1}
OUT=${1:?outdir}
mkdir -p "$OUT"
curl -sf "$B/releases/overview" > "$OUT/overview.json"
for v in $(jq -r '.[].release.name' "$OUT/overview.json"); do
  app=$(jq -r --arg v "$v" '.[] | select(.release.name==$v) | .release.konflux_application' "$OUT/overview.json")
  curl -sf "$B/releases/$v/staged" > "$OUT/staged-$v.json" || echo '{}' > "$OUT/staged-$v.json"
  curl -sf "$B/releases/$v/snapshots?application=$app&limit=1&offset=0" > "$OUT/latest-$v.json" || echo '{}' > "$OUT/latest-$v.json"
  snap=$(jq -r '.snapshots[0].name // empty' "$OUT/latest-$v.json")
  if [ -n "$snap" ]; then curl -sf "$B/releases/$v/snapshots/$snap" > "$OUT/snap-$v.json" || echo '{}' > "$OUT/snap-$v.json"; else echo '{}' > "$OUT/snap-$v.json"; fi
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$v" "$app" \
    "$(jq -r '.staged_image.name // "-"' "$OUT/staged-$v.json")" \
    "$(jq -r '.staged_fbc.name // "-"' "$OUT/staged-$v.json")" \
    "${snap:--}" \
    "$(jq -r '.fbc_catalog.status // "-"' "$OUT/snap-$v.json")" \
    "$(jq -r '.fbc_catalog.catalog_snapshot // "-"' "$OUT/snap-$v.json")"
done
