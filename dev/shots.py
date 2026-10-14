r"""Full-page screenshots and card text of release-readiness pages.

usage: python3 dev/shots.py [--route GLOB=FILE]... [--width PX]... <base-url> <out-dir> <path>...

Prerequisite: pip install --user playwright && python3 -m playwright install chromium

Example, a local backend on :8088 with quay-v3.17.6's candidate edited to need a re-run:

    curl -s localhost:8088/api/v1/releases/quay-v3.17.6/candidate |
      jq '.ci.refresh = "rerun" | .ci.passed = .ci.latest_in_ci' > /tmp/rerun.json
    python3 dev/shots.py --route '**/api/v1/releases/quay-v3.17.6/candidate=/tmp/rerun.json' \
      http://localhost:8088 /tmp/shots / /releases/quay-v3.17.6
"""
import argparse
import os
import re
import sys

from playwright.sync_api import Error, sync_playwright

# PatternFly scrolls main, not the document, so a full_page screenshot stops at the
# viewport: grow the viewport by main's overflow instead.
GROW = """() => {
  const m = document.querySelector('main.pf-v6-c-page__main');
  return m ? m.scrollHeight - m.clientHeight : 0;
}"""

# Card texts, and elements inside a card whose content is wider than their clipped box.
CARDS = """() => [...document.querySelectorAll('main .pf-v6-c-card')].map(c => ({
  text: c.innerText,
  overflow: [...c.querySelectorAll('*')].filter(e => e.scrollWidth > e.clientWidth + 1 &&
    getComputedStyle(e).overflowX !== 'visible').map(e => e.getAttribute('class') || e.tagName).slice(0, 5),
}))"""


def route(spec):
    """GLOB=FILE -> (GLOB, a handler answering with FILE's contents)."""
    glob, _, path = spec.rpartition("=")
    with open(path, "rb") as f:
        body = f.read()
    return glob, lambda r: r.fulfill(status=200, content_type="application/json", body=body)


def shoot(browser, url, png, routes, width):
    """Save url's whole page to png and print its console errors and cards; return why it did not load, or None."""
    with browser.new_context(viewport={"width": width, "height": 1000}, locale="en-US",
                             timezone_id="America/New_York") as ctx:
        for glob, handler in routes:
            ctx.route(glob, handler)
        page = ctx.new_page()
        errors = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        try:
            resp = page.goto(url, wait_until="networkidle")
        except Error as e:
            return e.message.partition("\n")[0]
        if resp.status >= 400:
            return f"HTTP {resp.status}"
        page.wait_for_timeout(1500)
        for _ in range(5):
            grow = page.evaluate(GROW)
            if grow <= 0:
                break
            page.set_viewport_size({"width": width, "height": page.viewport_size["height"] + grow})
            page.wait_for_timeout(500)
        page.screenshot(path=png)
        x_overflow = page.evaluate("() => document.documentElement.scrollWidth - document.documentElement.clientWidth")
        print(f"== {png} height={page.viewport_size['height']} console_errors={errors} page_x_overflow={x_overflow}")
        for i, card in enumerate(page.evaluate(CARDS)):
            print(f"-- card {i} overflow={card['overflow']}")
            print(card["text"])
    return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--route", action="append", default=[], type=route, metavar="GLOB=FILE",
                    help="answer requests matching GLOB with FILE as a 200 application/json response, "
                         "to render a state the live data lacks (a pick or re-run response saved with curl | jq)")
    ap.add_argument("--width", action="append", type=int, metavar="PX",
                    help="viewport width, repeatable (default 1440); with more than one, "
                         "each file name ends in -PX")
    ap.add_argument("base", metavar="base-url")
    ap.add_argument("out", metavar="out-dir")
    ap.add_argument("paths", metavar="path", nargs="+")
    args = ap.parse_args()
    widths = args.width or [1440]
    failed = False
    with sync_playwright() as p, p.chromium.launch() as browser:
        for path in args.paths:
            name = re.sub(r"[/?=&]", "_", path).strip("_") or "overview"
            for width in widths:
                suffix = f"-{width}" if len(widths) > 1 else ""
                png = os.path.join(args.out, name + suffix + ".png")
                err = shoot(browser, args.base.rstrip("/") + path, png, args.route, width)
                if err:
                    print(f"{path} at {width}px: {err}", file=sys.stderr)
                    failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
