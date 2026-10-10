# dev

Local tools for measuring the release page. None of them writes to a server.

- `cdp-shot.mjs`: screenshots a page in PNG chunks, writes an outline of its cards and tables with their y and height, and prints the page's console errors. It drives a headless Chrome or Chromium (Playwright's under `~/.cache/ms-playwright` works) started with `chrome --headless=new --remote-debugging-port=9222 --user-data-dir="$(mktemp -d)" about:blank`: `node dev/cdp-shot.mjs 9222 <url> <outdir> <prefix> [chunk-px] [click-text]`.
- `collect-fbc.sh`: for each release on the overview, prints the FBC the release page shows as one TSV line and saves the API JSON behind it: `B=http://127.0.0.1:8088/api/v1 dev/collect-fbc.sh <outdir>`.
- `fbc-ocp.sh`: prints the newest staged FBC per version, operator and OCP version, read from the cluster: `KUBECONFIG=<path> dev/fbc-ocp.sh`.
- `removed-lines.sh`: counts, per block, the lines the release page redesign deletes from `web/src/pages/ReleaseDetail.tsx`: `REV=HEAD dev/removed-lines.sh`.
