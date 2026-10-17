// Loads a built playground in headless Chromium, lets it run its first
// example, and checks the page: every code cell got a result, none failed,
// and each figure is an inline <svg>. Then it runs every other example.
//
//   node web/test/page.mjs <directory>   serve a built playground locally
//   node web/test/page.mjs <url>         check a deployed one
//
// It prints how long the runtime took to start and the first result to
// appear, counted from the start of the page load. It takes no screenshot.
import { chromium } from 'playwright';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';

const target = process.argv[2];
if (!target) {
  console.error('usage: node page.mjs <directory | url>');
  process.exit(2);
}

let server = null;
let url = target;
if (!/^https?:/.test(target)) {
  const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.wasm': 'application/wasm', '.md': 'text/markdown' };
  server = http.createServer((req, res) => {
    const p = path.join(target, decodeURIComponent(new URL(req.url, 'http://x').pathname));
    const file = fs.existsSync(p) && fs.statSync(p).isDirectory() ? path.join(p, 'index.html') : p;
    if (!fs.existsSync(file)) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { 'Content-Type': types[path.extname(file)] || 'application/octet-stream' });
    fs.createReadStream(file).pipe(res);
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  url = `http://127.0.0.1:${server.address().port}/`;
}

// Playwright's own Chromium, or else the installed Chrome.
const browser = await chromium.launch().catch(() => chromium.launch({ channel: 'chrome' }));
let failures = 0;
const fail = (msg) => {
  console.log('FAIL ' + msg);
  failures++;
};
try {
  const page = await browser.newPage();
  page.on('pageerror', (e) => fail('page error: ' + e.message));
  await page.goto(url);
  console.log('title:', await page.title());
  await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 120000 });
  const t = await page.evaluate(() => window.playgroundTimings);
  console.log(`runtime ready ${Math.round(t.runtimeReady)} ms, first result ${Math.round(t.firstResult)} ms after navigation start`);

  const names = await page.$$eval('#examples a', (as) => as.map((a) => a.dataset.name));
  for (const [i, name] of names.entries()) {
    if (i > 0) {
      await page.evaluate(() => (document.body.dataset.state = 'switching'));
      await page.evaluate((n) => (location.hash = n), name);
      await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 120000 });
    }
    const r = await page.evaluate(() => {
      const cells = [...document.querySelectorAll('#notebook .cell')];
      return {
        cells: cells.length,
        withResult: cells.filter((c) => c.querySelector('.meta').textContent !== '' && !c.classList.contains('stale')).length,
        errors: [...document.querySelectorAll('#notebook .output.error')].map((e) => e.textContent),
        figures: document.querySelectorAll('#notebook .figure').length,
        svgFigures: document.querySelectorAll('#notebook .figure > svg').length,
        status: document.getElementById('status').textContent,
      };
    });
    console.log(`${name}: ${r.cells} cells, ${r.withResult} with a result, ${r.figures} figures (${r.svgFigures} <svg>) — ${r.status}`);
    if (r.cells === 0) fail(`${name}: no code cell`);
    if (r.withResult !== r.cells) fail(`${name}: ${r.cells - r.withResult} cells without a result`);
    if (r.errors.length) fail(`${name}: ${r.errors.join('; ')}`);
    if (r.figures !== r.svgFigures) fail(`${name}: a figure is not an <svg>`);
  }
} finally {
  await browser.close();
  if (server) server.close();
}
process.exit(failures ? 1 : 0);
