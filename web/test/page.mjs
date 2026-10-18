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
  // The Ruby core must be fetched only once a notebook with a Ruby cell is
  // open, and how much of it crosses the wire is part of the report.
  let rubyFetched = null;
  page.on('requestfinished', async (req) => {
    if (!req.url().endsWith('/playground-ruby.wasm')) return;
    const sizes = await req.sizes().catch(() => null);
    const resp = await req.response();
    rubyFetched = {
      wire: sizes ? sizes.responseBodySize + sizes.responseHeadersSize : null,
      encoding: resp ? (await resp.allHeaders())['content-encoding'] || 'identity' : null,
    };
  });
  await page.goto(url);
  console.log('title:', await page.title());
  await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 120000 });
  const t = await page.evaluate(() => window.playgroundTimings);
  console.log(`runtime ready ${Math.round(t.runtimeReady)} ms, first result ${Math.round(t.firstResult)} ms after navigation start`);

  const names = await page.$$eval('#examples a', (as) => as.map((a) => a.dataset.name));
  let seenRuby = false;
  for (const [i, name] of names.entries()) {
    const ruby = await page.evaluate((n) => {
      const ex = JSON.parse(document.getElementById('examples-data').textContent).find((x) => x.name === n);
      return ex.cells.some((c) => c.lang === 'ruby');
    }, name);
    if (!ruby && rubyFetched && !seenRuby) fail(`${name}: the Ruby core was fetched for a notebook without Ruby`);
    if (i > 0) {
      const t0 = Date.now();
      await page.evaluate(() => (document.body.dataset.state = 'switching'));
      await page.evaluate((n) => (location.hash = n), name);
      await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 180000 });
      if (ruby) {
        seenRuby = true;
        const t = await page.evaluate(() => window.playgroundTimings);
        console.log(`${name}: Ruby core fetched (${rubyFetched ? rubyFetched.wire + ' bytes on the wire, ' + rubyFetched.encoding : 'not seen'}); first Ruby result ${Date.now() - t0} ms after opening the notebook (runtime ready ${Math.round(t.rubyReady)} ms after navigation start)`);
        if (!rubyFetched) fail(`${name}: has Ruby cells but the Ruby core was not fetched`);
      }
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
