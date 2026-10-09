#!/usr/bin/env bash
# Lines the release page redesign deletes from web/src/pages/ReleaseDetail.tsx
# at REV (default HEAD). Prints per-block counts and the total.
set -euo pipefail
REV=${REV:-HEAD}
F=web/src/pages/ReleaseDetail.tsx
git show "$REV:$F" | awk '
  function flush() { if (name != "") { printf "%-22s %4d  (%d-%d)\n", name, NR0 - start + 1, start, NR0; total += NR0 - start + 1; name = "" } }
  /^function (ReleaseSignal|pipelineSteps|StagedCard|StagedRow)\(/ { name = $2; sub(/\(.*/, "", name); start = NR }
  /^const step = \(/ { name = "step"; start = NR }
  /^\/\*\* ART build -> snapshot/ || /^\/\*\* The newest image and FBC Snapshots/ { doc++ }
  name != "" && /^}$/ { NR0 = NR; flush() }
  name == "step" && /^\) => / { NR0 = NR; flush() }
  /<Tabs defaultActiveKey="snapshot"/ { tstart = NR }
  tstart && /<\/Tabs>/ { printf "%-22s %4d  (%d-%d)\n", "Tabs block", NR - tstart + 1, tstart, NR; total += NR - tstart + 1; tstart = 0 }
  /<ReleaseSignal$/ { sstart = NR }
  sstart && /\/>$/ { printf "%-22s %4d  (%d-%d)\n", "ReleaseSignal mount", NR - sstart + 1, sstart, NR; total += NR - sstart + 1; sstart = 0 }
  END { printf "%-22s %4d  (+%d doc-comment lines)\n", "TOTAL", total + doc, doc }
'
