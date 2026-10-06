#!/usr/bin/env bash
# Re-records dev/e2e/fixtures from live Konflux (and JIRA when JIRA_TOKEN is
# set) using read-only GETs, trimmed to the fields the backend reads.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURES="$SCRIPT_DIR/fixtures"

KUBECONFIG_PATH="${KONFLUX_KUBECONFIG:-$HOME/.kube/configs/art.yaml}"
NAMESPACE="${KONFLUX_NAMESPACE:-art-quay-tenant}"
PER_APP="${PER_APP:-3}"
APPS='["quay-3-16","quay-3-17","quay-3-18","fbc-quay-3-16","fbc-quay-3-17","fbc-quay-3-18"]'

KUBECTL=(kubectl --kubeconfig "$KUBECONFIG_PATH" -n "$NAMESPACE")
if [[ -n "${KONFLUX_CONTEXT:-}" ]]; then
  KUBECTL+=(--context "$KONFLUX_CONTEXT")
fi

echo "record: listing snapshots in $NAMESPACE..."
"${KUBECTL[@]}" get snapshots.appstudio.redhat.com -o json |
  jq -S --argjson apps "$APPS" --argjson n "$PER_APP" '
    [.items[] | select(.spec.application as $a | $apps | index($a))]
    | group_by(.spec.application)
    | map(sort_by(.metadata.creationTimestamp, .metadata.name) | reverse | .[:$n])
    | flatten
    | map({
        metadata: {name: .metadata.name, creationTimestamp: .metadata.creationTimestamp},
        spec: {
          application: .spec.application,
          components: (.spec.components | sort_by(.name) | map({
            name, containerImage,
            source: {git: {url: .source.git.url, revision: .source.git.revision}}
          }))
        },
        status: {conditions: [.status.conditions[]? | select(.type == "AppStudioTestSucceeded") | {type, status}]}
      })
    | {metadata: {continue: ""}, items: .}
  ' >"$FIXTURES/konflux-snapshots.json"
echo "record: wrote $(jq '.items | length' "$FIXTURES/konflux-snapshots.json") snapshots"

if [[ -z "${JIRA_TOKEN:-}" ]]; then
  echo "record: JIRA_TOKEN unset, keeping hand-written JIRA fixtures"
  exit 0
fi

: "${JIRA_EMAIL:?JIRA_EMAIL is required with JIRA_TOKEN}"
JIRA_URL="${JIRA_URL:-https://redhat.atlassian.net}"
JIRA_PROJECT="${JIRA_PROJECT:-PROJQUAY}"
# Basic auth via a curl config on stdin keeps the token off argv.
jira_get() {
  printf 'user = "%s:%s"\n' "$JIRA_EMAIL" "$JIRA_TOKEN" |
    curl -fsS --config - -H 'Accept: application/json' -G "$JIRA_URL$1" "${@:2}"
}
# Same output whether issues have an assignee or not; real names are replaced.
SCRUB='.assignee |= (if . == null then null else {displayName: "Fixture Assignee"} end)'
# Stage JIRA output so a failed request leaves the committed fixtures intact.
STAGE="$(mktemp -d)"
trap 'rm -r -- "$STAGE"' EXIT

echo "record: recording JIRA releases..."
jira_get /rest/api/3/search/jql \
  --data-urlencode "jql=project=$JIRA_PROJECT AND component=\"-area/release\" AND status NOT IN (Closed, Done)" \
  --data-urlencode 'fields=summary,status,fixVersions,duedate,components,assignee' \
  --data-urlencode 'maxResults=100' |
  jq -S "{maxResults, issues: [.issues[] | {key, fields: (.fields | {summary, status: {name: .status.name}, fixVersions: [], duedate, components: [.components[] | {name}], assignee} | $SCRUB)}] | sort_by(.key)}" \
    >"$STAGE/jira-release-search.json"

# The backend builds fixVersions as "<product>-v<version>" from ticket summaries.
mapfile -t VERSIONS < <(jq -r '.issues[].fields.summary
  | capture("(?i)(?:(?<p>\\w+)\\s+)?v?(?<v>\\d+\\.\\d+(?:\\.\\d+)?)")
  | if (.p // "" | ascii_downcase) as $p | $p == "" or $p == "release" then .v else "\(.p | ascii_downcase)-v\(.v)" end' \
  "$STAGE/jira-release-search.json" | sort -u)

jira_get "/rest/api/3/project/$JIRA_PROJECT/versions" |
  jq -S --args '[.[] | select(.name as $n | $ARGS.positional | index($n)) | {name, description, releaseDate, released, archived}] | sort_by(.name)' \
    "${VERSIONS[@]}" >"$STAGE/jira-versions.json"

# Vulnerability summaries can carry embargoed details, so they are replaced.
for v in "${VERSIONS[@]}"; do
  echo "record: recording JIRA issues for $v..."
  jira_get /rest/api/3/search/jql \
    --data-urlencode "jql=project=$JIRA_PROJECT AND \"Target Version\"=\"$v\"" \
    --data-urlencode 'fields=summary,status,priority,labels,assignee,issuetype,resolution,updated' \
    --data-urlencode 'maxResults=100' |
    jq -S "{maxResults, issues: [.issues[] | {key, fields: (.fields | {summary: (if .issuetype.name == \"Vulnerability\" then \"Fixture vulnerability\" else .summary end), status: {name: .status.name}, priority: {name: .priority.name}, labels, assignee, issuetype: {name: .issuetype.name}, resolution: (.resolution | if . == null then null else {name} end), updated} | $SCRUB)}] | sort_by(.key)}" \
      >"$STAGE/jira-issues-$v.json"
done

rm -f "$FIXTURES"/jira-issues-*.json
mv "$STAGE"/*.json "$FIXTURES/"
