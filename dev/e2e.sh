#!/usr/bin/env bash
# Local e2e harness: builds the app, starts a fixture (or --live Konflux)
# backend on a free port with a scratch SQLite DB, waits for it to report
# data, then runs the web/ Playwright suite against it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ARTIFACTS_DIR="$REPO_ROOT/web/e2e-artifacts"

LIVE=0
NO_TESTS=0
KEEP=0
PASSTHROUGH_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --live) LIVE=1; shift ;;
    --no-tests) NO_TESTS=1; shift ;;
    --keep) KEEP=1; shift ;;
    *) PASSTHROUGH_ARGS+=("$1"); shift ;;
  esac
done

TMP_DIR="$(mktemp -d /tmp/release-readiness-e2e.XXXXXX)"
STUB_PID=""
BACKEND_PID=""

cleanup() {
  local status=$?
  if [[ "$KEEP" -eq 1 && "$status" -eq 0 ]]; then
    echo "e2e: --keep set, leaving backend (pid $BACKEND_PID) and stub (pid $STUB_PID) running"
    echo "e2e: backend URL: http://127.0.0.1:${BACKEND_PORT:-?}"
    echo "e2e: scratch dir left at $TMP_DIR"
    return
  fi
  [[ -n "$BACKEND_PID" ]] && kill "$BACKEND_PID" 2>/dev/null || true
  [[ -n "$BACKEND_PID" ]] && wait "$BACKEND_PID" 2>/dev/null || true
  [[ -n "$STUB_PID" ]] && kill "$STUB_PID" 2>/dev/null || true
  [[ -n "$STUB_PID" ]] && wait "$STUB_PID" 2>/dev/null || true
  rm -rf "$TMP_DIR"
  exit "$status"
}
trap cleanup EXIT

port_in_use() {
  ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE ":$1\$"
}

pick_port() {
  local port
  for _ in $(seq 1 100); do
    port=$(( (RANDOM % 20000) + 20000 ))
    if ! port_in_use "$port"; then
      echo "$port"
      return 0
    fi
  done
  echo "e2e: could not find a free port" >&2
  return 1
}

STUB_PORT="$(pick_port)"
BACKEND_PORT="$(pick_port)"
while [[ "$BACKEND_PORT" == "$STUB_PORT" ]]; do
  BACKEND_PORT="$(pick_port)"
done

mkdir -p "$ARTIFACTS_DIR"

# npm rewrites node_modules/.package-lock.json on every install, so this also
# catches a missing node_modules.
if [[ "$REPO_ROOT/web/package-lock.json" -nt "$REPO_ROOT/web/node_modules/.package-lock.json" ]]; then
  echo "e2e: installing web dependencies..."
  npm --prefix "$REPO_ROOT/web" ci
fi

if [[ "$NO_TESTS" -eq 0 ]]; then
  echo "e2e: ensuring Playwright Chromium is installed..."
  (cd "$REPO_ROOT/web" && npx playwright install chromium)
fi

echo "e2e: building web..."
npm --prefix "$REPO_ROOT/web" run build

echo "e2e: building release-readiness binary..."
go build -o "$TMP_DIR/release-readiness" "$REPO_ROOT/cmd/release-readiness/"
go build -o "$TMP_DIR/e2e-stub" "$REPO_ROOT/dev/e2e/stub/"

echo "e2e: starting fixture stub on 127.0.0.1:$STUB_PORT..."
"$TMP_DIR/e2e-stub" -addr "127.0.0.1:$STUB_PORT" -fixtures "$REPO_ROOT/dev/e2e/fixtures" \
  >"$TMP_DIR/stub.log" 2>&1 &
STUB_PID=$!

KUBECONFIG_PATH="$REPO_ROOT/dev/e2e/kubeconfig.yaml"
KONFLUX_CONTEXT_ARG=""
KONFLUX_NAMESPACE_ARG=""
E2E_MODE="fixture"

if [[ "$LIVE" -eq 1 ]]; then
  E2E_MODE="live"
  KUBECONFIG_PATH="${KONFLUX_KUBECONFIG:-$HOME/.kube/configs/art.yaml}"
  if [[ ! -f "$KUBECONFIG_PATH" ]]; then
    echo "e2e: --live requested but kubeconfig not found at $KUBECONFIG_PATH" >&2
    exit 1
  fi
  KONFLUX_CONTEXT_ARG="${KONFLUX_CONTEXT:-}"
  KONFLUX_NAMESPACE_ARG="${KONFLUX_NAMESPACE:-art-quay-tenant}"
else
  sed "s/__STUB_PORT__/$STUB_PORT/" "$REPO_ROOT/dev/e2e/kubeconfig.yaml" >"$TMP_DIR/kubeconfig.yaml"
  KUBECONFIG_PATH="$TMP_DIR/kubeconfig.yaml"
fi

JIRA_ARGS=(-jira-url "http://127.0.0.1:$STUB_PORT" -jira-token fixture)
# The backend reads JIRA_TOKEN/JIRA_URL from env, which keeps a real token off argv.
if [[ "$LIVE" -eq 1 && -n "${JIRA_TOKEN:-}" ]]; then
  JIRA_ARGS=()
fi

echo "e2e: starting backend on 127.0.0.1:$BACKEND_PORT (mode: $E2E_MODE)..."
env -u S3_BUCKET "$TMP_DIR/release-readiness" \
  -addr "127.0.0.1:$BACKEND_PORT" \
  -db "$TMP_DIR/release-readiness.db" \
  "${JIRA_ARGS[@]}" \
  -jira-poll-interval 2s \
  -konflux-kubeconfig "$KUBECONFIG_PATH" \
  -konflux-context "$KONFLUX_CONTEXT_ARG" \
  -konflux-namespace "$KONFLUX_NAMESPACE_ARG" \
  -konflux-poll-interval 2s \
  >"$TMP_DIR/backend.log" 2>&1 &
BACKEND_PID=$!

export E2E_BASE_URL="http://127.0.0.1:$BACKEND_PORT"
export E2E_MODE

# Fixture JIRA syncs one request per second, so wait for every fixture
# release to have its issues rather than for the first release.
RELEASES_FILTER='length'
MIN_RELEASES=1
if [[ "$LIVE" -eq 0 ]]; then
  RELEASES_FILTER='map(select(.issue_summary.total > 0)) | length'
  MIN_RELEASES="$(jq '.issues | length' "$REPO_ROOT/dev/e2e/fixtures/jira-release-search.json")"
fi

echo "e2e: waiting for readiness data..."
READY=0
for _ in $(seq 1 60); do
  if ! kill -0 "$BACKEND_PID" 2>/dev/null || ! kill -0 "$STUB_PID" 2>/dev/null; then
    echo "e2e: backend or stub exited early" >&2
    echo "--- backend.log ---" >&2
    cat "$TMP_DIR/backend.log" >&2
    echo "--- stub.log ---" >&2
    cat "$TMP_DIR/stub.log" >&2
    exit 1
  fi
  SNAPSHOTS_LEN="$(curl -fsS "$E2E_BASE_URL/api/v1/snapshots" 2>/dev/null | jq 'length' 2>/dev/null || echo 0)"
  RELEASES_LEN="$(curl -fsS "$E2E_BASE_URL/api/v1/releases/overview" 2>/dev/null | jq "$RELEASES_FILTER" 2>/dev/null || echo 0)"
  if [[ "${SNAPSHOTS_LEN:-0}" -gt 0 && "${RELEASES_LEN:-0}" -ge "$MIN_RELEASES" ]]; then
    READY=1
    break
  fi
  sleep 1
done

if [[ "$READY" -ne 1 ]]; then
  echo "e2e: timed out waiting for readiness (snapshots=$SNAPSHOTS_LEN releases=$RELEASES_LEN)" >&2
  echo "--- backend.log ---" >&2
  cat "$TMP_DIR/backend.log" >&2
  echo "--- stub.log ---" >&2
  cat "$TMP_DIR/stub.log" >&2
  exit 1
fi

echo "e2e: ready (snapshots=$SNAPSHOTS_LEN releases=$RELEASES_LEN) at $E2E_BASE_URL"

if [[ "$NO_TESTS" -eq 1 ]]; then
  echo "e2e: --no-tests set, skipping Playwright run"
  exit 0
fi

npm --prefix "$REPO_ROOT/web" run e2e -- "${PASSTHROUGH_ARGS[@]}"
