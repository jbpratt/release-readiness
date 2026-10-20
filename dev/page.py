r"""One self-contained HTML page of the app on recorded data, for the design gallery.

usage: python3 dev/page.py --fixtures FILE --path PATH --option KEY [--recommended] [--title T] [--build DIR] <out.html>

FILE maps /api/v1/ paths to responses, as `dev/shots.py --record FILE` writes it. It holds only
what the shots fetched before the screenshot: a fetch a click makes (the Release inputs dialog's
/api/v1/releases/<v>/fbc-tags) is missing until merged in by hand, e.g. with jq. The page
is the app built with `vite build --mode page` (or the page build in --build DIR) showing PATH,
with its JS, CSS, fixtures and favicon inline: it needs no network and no origin.

Example, the overview and one release from a local backend on :8088:

    python3 dev/shots.py --record /tmp/fx.json http://localhost:8088 /tmp/shots / /releases/quay-v3.17.6
    python3 dev/page.py --fixtures /tmp/fx.json --path /releases/quay-v3.17.6 --option A /tmp/release.html
"""
import argparse
import base64
import html
import json
import os
import re
import subprocess
import sys
import tempfile

WEB = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "web")
MAX_BYTES = 16 * 1024 * 1024

# Refused in the output. AKIA and ATATT3 take a token-length tail: bare, 4 to 6 characters can
# occur by chance in the bundle's base64 fonts.
SECRETS = [
    ("a GitHub token", r"gh[pousr]_|github_pat_"),
    ("a GitLab token", r"glpat-"),
    ("a Slack token", r"xox[a-z]-"),
    ("an OpenShift token", r"sha256~"),
    ("a private key", r"PRIVATE KEY"),
    ("a JWT", r"eyJ[A-Za-z0-9_-]*\.eyJ"),
    ("a docker auths block", r'\\?"auths\\?"\s*:'),
    ("an AWS access key", r"AKIA[0-9A-Z]{16}"),
    ("an Atlassian token", r"ATATT3[A-Za-z0-9_=-]{20}"),
]


def scrub(v, key=None):
    """Replace people's names with a role, recursively."""
    if isinstance(v, dict):
        return {k: scrub(x, k) for k, x in v.items()}
    if isinstance(v, list):
        return [scrub(x) for x in v]
    if key == "release_ticket_assignee" and v:
        return "Release Engineer"
    return v


def inline_safe(s, tag):
    """s with nothing that ends the <tag> it is inlined in, or escapes a script's text."""
    return re.sub(rf"</({tag})|<!--", lambda m: f"<\\/{m[1]}" if m[1] else "<\\!--", s, flags=re.I)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def render(build, fixtures, args):
    index = read(os.path.join(build, "index.html"))
    with open(os.path.join(build, "favicon.png"), "rb") as f:
        favicon = "data:image/png;base64," + base64.b64encode(f.read()).decode()
    (js,) = re.findall(r'<script type="module" crossorigin src="/([^"]+)"></script>', index)
    (css,) = re.findall(r'<link rel="stylesheet" crossorigin href="/([^"]+)">', index)
    data = json.dumps(fixtures, separators=(",", ":")).replace("<", "\\u003c")
    path = json.dumps(args.path).replace("<", "\\u003c")
    head = "\n".join([
        f'<meta name="gc-option" content="{html.escape(args.option)}" />',
        *(['<meta name="gc-recommended" content="true" />'] if args.recommended else []),
        '<meta name="gc-theme" content="both" />',
        f"<script>window.__RR_PAGE_PATH__ = {path};\nwindow.__RR_FIXTURES__ = {data};</script>",
        f"<style>{inline_safe(read(os.path.join(build, css)), 'style')}</style>",
        f"<script type=\"module\">{inline_safe(read(os.path.join(build, js)).replace('/favicon.png', favicon), 'script')}</script>",
    ])
    # Drop the theme script (it reads localStorage, which throws with no origin) and the
    # asset links, then add the inlined head; the app picks the theme itself.
    index, n = re.subn(r"<script>\s*\(\(\) => \{.*?</script>", "", index, flags=re.S)
    if n != 1:
        sys.exit(f"page.py: found {n} theme scripts in index.html, want 1")
    index = re.sub(r'\s*<script type="module"[^>]*></script>|\s*<link rel="stylesheet"[^>]*>', "", index)
    index = index.replace('href="/favicon.png"', f'href="{favicon}"')
    if args.title:
        index = re.sub(r"<title>.*?</title>", lambda _: f"<title>{html.escape(args.title)}</title>", index)
    return index.replace("</head>", head + "\n  </head>", 1)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--fixtures", required=True, metavar="FILE", help="JSON from dev/shots.py --record")
    ap.add_argument("--path", required=True, help="app path the page shows, e.g. / or /mockups/x")
    ap.add_argument("--option", required=True, metavar="KEY", help="gc-option key, e.g. A")
    ap.add_argument("--recommended", action="store_true", help="mark the page as the lane's pick")
    ap.add_argument("--title", help="page title (default the app's)")
    ap.add_argument("--build", metavar="DIR", help="an existing vite build --mode page output, instead of building")
    ap.add_argument("out", metavar="out.html")
    args = ap.parse_args()

    with open(args.fixtures, encoding="utf-8") as f:
        fixtures = scrub(json.load(f))
    for p in fixtures.get("/api/v1/sync-status", {}).get("problems") or []:
        p["message"] = "sync failed"

    with tempfile.TemporaryDirectory() as tmp:
        build = args.build
        if not build:
            build = tmp
            subprocess.run(["npx", "vite", "build", "--mode", "page", "--outDir", tmp, "--emptyOutDir",
                            "--logLevel", "warn"], cwd=WEB, check=True)
        page = render(build, fixtures, args)

    for kind, pattern in SECRETS:
        if re.search(pattern, page):
            print(f"page.py: refusing to write {args.out}: it contains {kind} shape", file=sys.stderr)
            return 1
    out = page.encode("utf-8")
    if len(out) > MAX_BYTES:
        print(f"page.py: refusing to write {args.out}: {len(out)} bytes is over 16 MiB", file=sys.stderr)
        return 1
    with open(args.out, "wb") as f:
        f.write(out)
    print(f"{args.out}: {len(out)} bytes")
    return 0


if __name__ == "__main__":
    sys.exit(main())
