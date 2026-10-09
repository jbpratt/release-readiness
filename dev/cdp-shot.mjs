// Usage: node cdp-shot.mjs <port> <url> <outdir> <prefix> [chunkPx] [clickText]
// Renders url at 1440 px wide with the viewport stretched to the full content
// height, writes <prefix>-outline.json (cards, tabs, tables with y and height)
// and PNG chunks <prefix>-NN.png of chunkPx height (default 1400).
// clickText, when given, clicks the first button/tab whose text equals it first.
// Prints the content height, chunk count and the page's console errors.
const [port, url, outdir, prefix, chunkArg, clickText] = process.argv.slice(2);
const chunk = Number(chunkArg || 1400);
const fs = await import("node:fs");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let targets;
for (let i = 0; i < 50; i++) {
  try { targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json(); break; } catch { await sleep(200); }
}
const page = targets.find((t) => t.type === "page");
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let id = 0; const pending = new Map(); const errors = [];
ws.addEventListener("message", (ev) => {
  const m = JSON.parse(ev.data), p = m.params;
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
  else if (m.method === "Runtime.exceptionThrown") errors.push(p.exceptionDetails.exception?.description ?? p.exceptionDetails.text);
  else if (m.method === "Runtime.consoleAPICalled" && p.type === "error") errors.push(p.args.map((a) => a.value ?? a.description).join(" "));
  else if (m.method === "Log.entryAdded" && p.entry.level === "error") errors.push(`${p.entry.text} ${p.entry.url ?? ""}`.trim());
});
const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
const evalJS = async (expr) => (await send("Runtime.evaluate", { expression: expr, returnByValue: true })).result.result.value;
await send("Page.enable");
await send("Runtime.enable");
await send("Log.enable");
await send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
errors.length = 0; // enabling Runtime and Log replays the previous page's messages
await send("Page.navigate", { url });
await sleep(12000);
if (clickText) {
  await evalJS(`(() => { const el = [...document.querySelectorAll('button')].find(b => b.innerText.trim() === ${JSON.stringify(clickText)}); if (el) el.click(); return !!el; })()`);
  await sleep(4000);
}
const fullH = await evalJS(`Math.max(...[...document.querySelectorAll('*')].map(e => e.scrollHeight))`);
await send("Emulation.setDeviceMetricsOverride", { width: 1440, height: fullH + 100, deviceScaleFactor: 1, mobile: false });
await sleep(2000);
const info = await evalJS(`(() => {
  const out = [];
  for (const el of document.querySelectorAll('h1,.pf-v6-c-card,.pf-v6-c-tabs__item-text,table')) {
    const r = el.getBoundingClientRect();
    if (!r.height) continue;
    let text;
    if (el.classList.contains('pf-v6-c-card')) {
      const t = el.querySelector('.pf-v6-c-card__title');
      text = 'CARD: ' + (t ? t.innerText.trim().replace(/\\s+/g, ' ').slice(0, 110) : '(untitled)');
    } else if (el.tagName === 'TABLE') {
      text = 'TABLE rows=' + el.querySelectorAll('tbody tr').length + ' cols=' + [...el.querySelectorAll('thead th')].map(t => t.innerText.trim()).join('|');
    } else text = el.tagName + ': ' + el.innerText.trim().slice(0, 100);
    out.push({ y: Math.round(r.top), h: Math.round(r.height), text });
  }
  const bottom = Math.max(...[...document.querySelectorAll('.pf-v6-c-card, h1')].map(e => e.getBoundingClientRect().bottom));
  return { outline: out, contentH: Math.round(bottom) };
})()`);
fs.writeFileSync(`${outdir}/${prefix}-outline.json`, JSON.stringify(info, null, 1));
const total = info.contentH + 40;
const n = Math.ceil(total / chunk);
for (let i = 0; i < n; i++) {
  const y = i * chunk, h = Math.min(chunk, total - y);
  const shot = await send("Page.captureScreenshot", { format: "png", clip: { x: 0, y, width: 1440, height: h, scale: 1 }, captureBeyondViewport: true });
  fs.writeFileSync(`${outdir}/${prefix}-${String(i).padStart(2, "0")}.png`, Buffer.from(shot.result.data, "base64"));
}
console.log(JSON.stringify({ contentH: info.contentH, chunks: n, errors }));
ws.close();
