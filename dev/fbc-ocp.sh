#!/usr/bin/env bash
# Prints, for each version, the newest staged FBC per operator and OCP version,
# read from the cluster (read-only).
# Only a successful FBC Release reports its OCP version, so failed ones are left out.
set -euo pipefail
NS=${NS:-art-quay-tenant}
VERSIONS=${VERSIONS:-3.18.1 3.17.6}
jq -n --tab --arg versions "$VERSIONS" \
  --slurpfile s <(kubectl -n "$NS" get snapshots.appstudio.redhat.com -o json) \
  --slurpfile r <(kubectl -n "$NS" get releases.appstudio.redhat.com -o json) '
  ($versions | split(" ")) as $vs
  | ($r[0].items | group_by(.spec.snapshot)
      | map(max_by(.metadata.creationTimestamp) | {key: .spec.snapshot, value: .}) | from_entries) as $newest
  | {captured_at: (now | todate), catalogs: [
      $s[0].items[] | .metadata.annotations as $a
      | select($a["art.redhat.com/env"] == "stage" and $a["art.redhat.com/kind"] == "fbc"
          and ($a["art.redhat.com/assembly"] | IN($vs[])))
      | $newest[.metadata.name] as $rel
      | select($rel.status.artifacts.components[0].ocp_version != null)
      | {version: $a["art.redhat.com/assembly"],
         operator: (.spec.components[0].name | sub("^fbc-quay-[0-9]+-[0-9]+-"; "")),
         ocp: $rel.status.artifacts.components[0].ocp_version,
         name: .metadata.name,
         created_at: .metadata.creationTimestamp,
         status: ($rel.status.conditions[] | select(.type == "Released") | .reason)}
    ] | group_by([.version, .operator, .ocp]) | map(max_by(.created_at))
      | sort_by(.version, .operator, (.ocp | ltrimstr("v") | split(".") | map(tonumber)))}'
