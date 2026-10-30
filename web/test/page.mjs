// Loads a built playground in headless Chromium, lets it run its first
// example, and checks the page: every code cell got a result, none failed,
// and each figure is an inline <svg>. Then it runs every other example.
// Before that it drives the loading carousel: shown on a cold load, never on
// a cached one, gone when the runtime is ready, worked by the keyboard, and
// still under prefers-reduced-motion.
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
import os from 'node:os';

const target = process.argv[2];
if (!target) {
  console.error('usage: node page.mjs <directory | url>');
  process.exit(2);
}

let server = null;
let url = target;
// How many times the local server sent each file: a load served from the
// browser's cache sends nothing.
const served = new Map();
if (!/^https?:/.test(target)) {
  const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.wasm': 'application/wasm', '.md': 'text/markdown', '.svg': 'image/svg+xml' };
  server = http.createServer((req, res) => {
    const name = decodeURIComponent(new URL(req.url, 'http://x').pathname);
    const p = path.join(target, name);
    const file = fs.existsSync(p) && fs.statSync(p).isDirectory() ? path.join(p, 'index.html') : p;
    if (!fs.existsSync(file)) {
      res.writeHead(404).end();
      return;
    }
    served.set(path.basename(name), (served.get(path.basename(name)) || 0) + 1);
    // Cacheable, as GitHub Pages serves it (max-age=600), so that a second
    // load can come from the cache.
    res.writeHead(200, { 'Content-Type': types[path.extname(file)] || 'application/octet-stream', 'Cache-Control': 'max-age=600' });
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
// ---- The loading carousel ------------------------------------------------
//
// The landing kit's carousel shows what go-notebook does while the core is
// still downloading after 300 ms. A slow network is made by holding the
// core's request back for HOLD ms, longer than one rotation (4.5 s), so that
// a rotation can be seen, or seen not to happen. The cold and cached loads
// run in a profile of their own, on disk: only a disk cache holds a 30 MB
// core, and a second load must be able to come from it.
const HOLD = 5200;
// A code cell's language badge (prose cells have one too, and so do the
// fences in prose).
const CODE_BADGES = '#notebook .cell[data-lang] .cell-head .lang-badge';
const corePattern = /\/playground(-ruby)?\.wasm(\?.*)?$/;
async function holdCore(context) {
  await context.route(corePattern, async (route) => {
    await new Promise((r) => setTimeout(r, HOLD));
    await route.continue();
  });
}
async function open(context, label) {
  const page = context.pages()[0] || (await context.newPage());
  page.on('pageerror', (e) => fail(`${label}: page error: ${e.message}`));
  await page.goto(url);
  return page;
}
const waitReady = (page) => page.waitForFunction(() => window.playgroundTimings && window.playgroundTimings.runtimeReady !== null, null, { timeout: 120000 });
const state = (page) => page.evaluate(() => ({ ...window.landingkitCarousel.state }));
// whether the fade (the kit's lk-car-fading class on #loading) ever ran
const watchFade = (page) =>
  page.evaluate(() => {
    window.carouselFaded = false;
    const box = document.getElementById('loading');
    new MutationObserver(() => {
      if (box.classList.contains('lk-car-fading')) window.carouselFaded = true;
    }).observe(box, { attributes: true, attributeFilter: ['class'] });
  });
const visibleSlide = (page) =>
  page.$eval('#lk-carousel', (s) => [...s.querySelectorAll('.lk-car-card')].filter((c) => !c.hidden).map((c) => c.getAttribute('aria-label')).join('|'));

async function carouselChecks() {
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'playground-profile-'));
  const launch = (opts) =>
    chromium.launchPersistentContext(profile, opts).catch(() => chromium.launchPersistentContext(profile, { ...opts, channel: 'chrome' }));
  let context = await launch({});
  try {
    // 1. A cold load, left alone: the carousel shows, rotates, and goes away
    // (fading) when the runtime is ready.
    await holdCore(context);
    let page = await open(context, 'cold');
    if (!(await page.$('#lk-carousel'))) {
      fail('the page has no loading carousel');
      return;
    }
    await page.waitForSelector('#lk-carousel:not([hidden])', { timeout: HOLD }).catch(() => fail('cold load: the carousel did not show while the core downloaded'));
    await watchFade(page);
    // While the core is held back, every cell's badge says it cannot run yet.
    const waiting = await page.$$eval(CODE_BADGES, (bs) => bs.map((b) => [b.dataset.runnable, b.textContent]));
    console.log('badges while the core downloads: ' + [...new Set(waiting.map((w) => w[1]))].join(' | '));
    if (!waiting.length || !waiting.every(([r, t]) => r === 'no' && /· (loading the (Ruby )?runtime…|waits for the Ruby runtime|not runnable yet)$/.test(t)))
      fail('a badge does not say its cell cannot run yet: ' + JSON.stringify(waiting));
    const label = await page.$eval('#lk-carousel', (s) => s.getAttribute('aria-label'));
    const n = await page.$$eval('#lk-carousel .lk-car-card', (cs) => cs.length);
    console.log(`carousel: "${label}", ${n} cards, shown on a cold load`);
    if (n < 3) fail(`the carousel has ${n} cards`);
    const pics = await page.$$eval('#lk-carousel .lk-car-card img', (is) => is.map((i) => i.getAttribute('src')));
    if (!pics.every((p) => /^cards\/\d+\.svg\?v=[0-9a-f]{12}$/.test(p))) fail('a card picture is not cards/<n>.svg?v=<hash>: ' + pics.join(' '));
    await waitReady(page);
    await page.waitForFunction(() => document.getElementById('loading').hidden, null, { timeout: 3000 }).catch(() => fail('cold load: the carousel stayed after the runtime was ready'));
    let st = await state(page);
    const lag = await page.evaluate(() => performance.now() - window.playgroundTimings.runtimeReady);
    console.log(`carousel: gone ${Math.round(lag)} ms after the runtime was ready; rotated ${st.advanced} times`);
    if (!st.shown || !st.downloaded || st.startShown) fail('cold load: state ' + JSON.stringify(st));
    if (st.advanced < 1) fail('cold load: the carousel never rotated in ' + HOLD + ' ms');
    if (!(await page.evaluate(() => window.carouselFaded))) fail('cold load: the carousel did not fade');

    // 2. The same profile again, the core in the cache: no carousel at all.
    await context.unroute(corePattern);
    const before = served.get('playground.wasm') || 0;
    await page.goto('about:blank');
    page = await open(context, 'cached');
    await waitReady(page);
    st = await state(page);
    const fetched = (served.get('playground.wasm') || 0) - before;
    console.log(`carousel: cached load, shown ${st.shown}` + (server ? `, core sent ${fetched} times` : ''));
    if (st.shown) fail('cached load: the carousel flashed');
    if (server && fetched !== 0) fail(`cached load: the core was sent again (${fetched}): this was not a cached load`);
    if (!(await page.evaluate(() => document.getElementById('loading').hidden))) fail('cached load: #loading is not hidden');
  } finally {
    await context.close();
    fs.rmSync(profile, { recursive: true, force: true });
  }

  // 3. The keyboard, in a fresh context: the rotation button comes first;
  // focus inside stops the rotation and makes the slides live; Enter and
  // Space work the buttons; hidden slides take no focus. A reader in the
  // carousel is not interrupted: the Start button waits, and moves focus to
  // the notebook.
  const kctx = await browser.newContext();
  try {
    await holdCore(kctx);
    const page = await open(kctx, 'keyboard');
    await page.waitForSelector('#lk-carousel:not([hidden])', { timeout: HOLD });
    const first = await page.$eval('#lk-carousel', (s) => s.querySelector('button, a:not([tabindex="-1"])').id);
    if (first !== 'lk-carousel-rot') fail('the first control of the region is ' + first);
    await page.focus('#lk-carousel-rot');
    const active = () => page.evaluate(() => {
      const a = document.activeElement;
      return a.id || a.getAttribute('aria-label') || a.textContent.trim();
    });
    const rot = () => page.$eval('#lk-carousel-rot', (b) => b.getAttribute('aria-label'));
    if ((await rot()) !== 'Stop slide rotation') fail('focusing the rotation button stopped the rotation');
    await page.keyboard.press('Tab');
    const link = await page.evaluate(() => {
      const a = document.activeElement;
      return a.tagName === 'A' && !a.closest('.lk-car-card').hidden;
    });
    if (!link) fail('Tab after the rotation button is not the visible card\'s link: ' + (await active()));
    if ((await rot()) !== 'Start slide rotation') fail('focus in the carousel did not stop the rotation');
    if ((await page.$eval('#lk-carousel-slides', (d) => d.getAttribute('aria-live'))) !== 'polite') fail('stopped, the slides are not aria-live polite');
    const order = [];
    for (let i = 0; i < 12 && (await active()) !== 'lk-carousel-next'; i++) {
      await page.keyboard.press('Tab');
      order.push(await active());
    }
    if ((await active()) !== 'lk-carousel-next') fail('Tab never reached the next button: ' + order.join(', '));
    console.log('carousel: tab order rotation, link, ' + order.join(', '));
    const s0 = await visibleSlide(page);
    await page.keyboard.press('Enter');
    const s1 = await visibleSlide(page);
    if (s1 === s0 || !/^2 of /.test(s1)) fail(`Enter on next: ${s0} -> ${s1}`);
    const dot = await page.$$eval('.lk-car-dots button', (ds) => ds.findIndex((d) => d.getAttribute('aria-disabled') === 'true'));
    if (dot !== 1) fail('the current dot is ' + dot);
    await page.focus('#lk-carousel-prev');
    await page.keyboard.press('Space');
    if (!/^1 of /.test(await visibleSlide(page))) fail('Space on previous did not go back');
    await page.focus('#lk-carousel-next');
    await waitReady(page);
    await page.waitForSelector('#lk-carousel-start:not([hidden])', { timeout: 3000 }).catch(() => fail('a reader in the carousel got no Start button'));
    if (await page.evaluate(() => document.getElementById('loading').hidden)) fail('the carousel went away under a reader');
    await page.keyboard.press('Tab');
    if ((await active()) !== 'lk-carousel-start') fail('Tab after next is not the Start button: ' + (await active()));
    await page.keyboard.press('Enter');
    if (!(await page.evaluate(() => document.getElementById('loading').hidden))) fail('Start did not close the carousel');
    if ((await active()) !== 'notebook') fail('after Start, focus is on ' + (await active()));
  } finally {
    await kctx.close();
  }

  // 4. Reduced motion: shown, but stopped from the start (live slides), never
  // rotated, and gone at once, without the fade.
  // (At a phone's width, which the badges must fit too.)
  const rctx = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 360, height: 740 } });
  try {
    await holdCore(rctx);
    const page = await open(rctx, 'reduced motion');
    await page.waitForSelector('#lk-carousel:not([hidden])', { timeout: HOLD });
    await watchFade(page);
    const label = await page.$eval('#lk-carousel-rot', (b) => b.getAttribute('aria-label'));
    if (label !== 'Start slide rotation') fail('reduced motion: the carousel rotates (' + label + ')');
    await waitReady(page);
    await page.waitForFunction(() => document.getElementById('loading').hidden, null, { timeout: 3000 }).catch(() => fail('reduced motion: the carousel stayed'));
    const st = await state(page);
    console.log(`carousel: reduced motion, rotated ${st.advanced} times, faded ${await page.evaluate(() => window.carouselFaded)}`);
    if (st.advanced !== 0) fail('reduced motion: the carousel rotated');
    if (await page.evaluate(() => window.carouselFaded)) fail('reduced motion: the carousel faded');
    const fit = await page.evaluate(() => ({
      over: [...document.querySelectorAll('.lang-badge')].filter((b) => b.getBoundingClientRect().right > document.documentElement.clientWidth).length,
      badges: document.querySelectorAll('.lang-badge').length,
      scroll: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    }));
    if (!fit.badges || fit.over || fit.scroll) fail('at 360 px, the badges do not fit: ' + JSON.stringify(fit));
  } finally {
    await rctx.close();
  }
}

// ---- The Julia example ----------------------------------------------------

// juliaExampleChecks reads the open notebook, an example with Julia cells: its
// Julia cells say they are checked and not run, in the neutral style and not as
// a failure; a Go cell that reads one is blocked, also not a failure; a Go cell
// that does not runs and shows a value; and the prose says plainly that Julia
// is checked, not run, and links go-embedded-julia.
async function juliaExampleChecks(page, name) {
  const r = await page.evaluate(() => {
    const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
    return {
      cells: cells.map((c) => ({
        lang: c.dataset.lang,
        out: (c.querySelector('.outputs').textContent || '').trim(),
        notRunStyle: c.querySelectorAll('.outputs .output.skipped').length,
        failStyle: c.querySelectorAll('.outputs .output.error').length,
      })),
      prose: [...document.querySelectorAll('#notebook .prose')].map((p) => p.textContent).join('\n'),
      links: [...document.querySelectorAll('#notebook .prose a')].map((a) => a.getAttribute('href')),
    };
  });
  const julia = r.cells.filter((c) => c.lang === 'julia');
  const blocked = r.cells.filter((c) => c.lang !== 'julia' && c.out.includes('blocked: it depends on cell'));
  const alone = r.cells.filter((c) => c.lang !== 'julia' && c.out !== '' && !c.out.includes('blocked:') && !c.failStyle);
  console.log(`${name}: Julia example: ${julia.length} Julia, ${blocked.length} blocked, ${alone.length} independent`);
  if (!julia.length || julia.some((c) => !c.out.includes('Julia cells are parsed and checked, but not run yet') || !c.notRunStyle || c.failStyle))
    fail(`${name}: a Julia cell should say it is checked, not run, and not look failed: ` + JSON.stringify(julia));
  if (!blocked.length || blocked.some((c) => !c.notRunStyle || c.failStyle))
    fail(`${name}: no Go cell is blocked on the Julia cell, or it looks failed: ` + JSON.stringify(r.cells));
  if (!alone.length) fail(`${name}: no Go cell runs independently of the Julia cell: ` + JSON.stringify(r.cells));
  if (!/checked, not run/.test(r.prose) || !r.links.includes('https://go-embedded-julia.github.io/'))
    fail(`${name}: the prose should say Julia cells are checked, not run, and link go-embedded-julia.github.io`);
  if (!/line 2:8: syntax error/.test(r.prose)) fail(`${name}: the prose should show a syntax error caught at its line`);
}

// ---- Editing --------------------------------------------------------------

const CODE_CELLS = '#notebook .cell[data-lang]';
const DONE = 'body[data-state="done"], body[data-state="error"]';
const settle = (page) => page.evaluate(() => (document.body.dataset.state = 'switching'));
async function waitDone(page) {
  await page.waitForSelector(DONE, { timeout: 180000 });
}
// downloadBytes clicks Download and returns the file the browser saves.
async function downloadBytes(page) {
  const [d] = await Promise.all([page.waitForEvent('download'), page.click('#download')]);
  return fs.readFileSync(await d.path());
}
// ranMs does something that runs cells and returns the run's time, as the
// status line says it.
async function ranMs(page, act) {
  await settle(page);
  await act();
  await waitDone(page);
  const s = await page.textContent('#status');
  const m = s.match(/Ran \d+ cells? in ([\d.]+) (ms|s)\./);
  if (!m) {
    fail('no run time in the status: ' + s);
    return NaN;
  }
  return m[2] === 's' ? +m[1] * 1000 : +m[1];
}
async function openFile(page, name, text) {
  await settle(page);
  await page.setInputFiles('#open', { name, mimeType: 'text/markdown', buffer: Buffer.isBuffer(text) ? text : Buffer.from(text) });
}
const codeState = (page) =>
  page.$$eval(CODE_CELLS, (cs) =>
    cs.map((c) => ({ lang: c.dataset.lang, out: (c.querySelector('.outputs').textContent || '').trim(), note: (c.querySelector('.cell-note') || {}).textContent || '' }))
  );

// A visitor adds a Ruby cell, types into it, runs it, makes it Go, deletes
// and brings it back, moves it, and downloads the notebook, which reads back
// byte for byte; all from the + control, the cell's own controls and
// Jupyter's keys.
async function editingChecks(page) {
  const start = '# edit\n\nSome text.\n\n~~~go {.opt}\nx := 21\n~~~\n';
  await openFile(page, 'edit.md', start);
  await waitDone(page);
  // prose shows as text until Edit (or Enter on it) opens its editor
  const proseEditors = await page.$$eval('.prose-cell textarea', (ts) => ts.filter((t) => t.offsetParent !== null).length);
  if (proseEditors) fail(`${proseEditors} prose editors are open before anyone asked`);
  await page.locator('.prose-cell .actions button', { hasText: 'Edit' }).click();
  if (!(await page.evaluate(() => document.activeElement.closest('.prose-cell') && document.activeElement.tagName === 'TEXTAREA')))
    fail('Edit on a Markdown cell does not open its editor');
  await page.keyboard.press('Escape');
  if (await page.$eval('.prose-cell textarea', (t) => t.offsetParent !== null)) fail('Esc does not close a Markdown editor');
  if ((await downloadBytes(page)).toString() !== start) fail('an opened notebook does not download as it was');

  // + Add cell at the end: the menu offers the cell above's language first
  await page.click('#add-end');
  const kinds = await page.$$eval('.add-menu:not([hidden]) button', (bs) => bs.map((b) => b.textContent));
  console.log('add menu: ' + kinds.join(', '));
  if (kinds.join() !== 'Go (as above),Ruby,Julia · not runnable yet,Markdown') fail('the + menu offers ' + kinds.join(', '));
  await page.click('.add-menu:not([hidden]) button[data-kind="ruby"]');
  await page.waitForFunction(() => {
    const a = document.activeElement;
    return a && a.tagName === 'TEXTAREA' && a.closest('.cell').dataset.lang === 'ruby';
  });
  const mode = await page.evaluate(() => document.activeElement.closest('.cell').querySelector('.mode').textContent);
  if (!/^editing/.test(mode)) fail('a cell being edited does not say so: ' + mode);
  await page.keyboard.type('x * 2');
  await settle(page);
  await page.keyboard.press('Shift+Enter'); // run it, and move on
  await waitDone(page);
  let cells = await codeState(page);
  console.log('after adding a Ruby cell: ' + JSON.stringify(cells));
  if (cells.length !== 2 || cells[1].lang !== 'ruby' || cells[1].out !== '42') fail('the new Ruby cell did not run to 42: ' + JSON.stringify(cells));

  // its language, through its badge: the fence changes, the code does not
  await settle(page);
  await page.selectOption(CODE_CELLS + '[data-lang="ruby"] .lang-select', 'go');
  await waitDone(page);
  cells = await codeState(page);
  console.log('after Ruby -> Go: ' + JSON.stringify(cells));
  if (cells[1].lang !== 'go' || cells[1].out !== '42' || !/not translated/.test(cells[1].note)) fail('Ruby -> Go: ' + JSON.stringify(cells));


  // D D deletes the selected cell, Z brings it back
  await page.evaluate((s) => [...document.querySelectorAll(s)].pop().focus(), CODE_CELLS);
  await page.keyboard.press('d');
  await page.keyboard.press('d');
  await page.waitForFunction((s) => document.querySelectorAll(s).length === 1, CODE_CELLS);
  if (await page.isHidden('#undo')) fail('no Undo button after a delete');
  await page.keyboard.press('z');
  await page.waitForFunction((s) => document.querySelectorAll(s).length === 2, CODE_CELLS);
  // B adds a cell below in the language above, Esc selects it, D D deletes it
  await page.evaluate((s) => [...document.querySelectorAll(s)].pop().focus(), CODE_CELLS);
  await page.keyboard.press('b');
  await page.waitForFunction((s) => document.querySelectorAll(s).length === 3 && document.activeElement.tagName === 'TEXTAREA', CODE_CELLS);
  await page.keyboard.press('Escape');
  await page.keyboard.press('d');
  await page.keyboard.press('d');
  await page.waitForFunction((s) => document.querySelectorAll(s).length === 2, CODE_CELLS);

  // the download, and the same file opened and downloaded again
  const out = (await downloadBytes(page)).toString();
  const want = '# edit\n\nSome text.\n\n~~~go {.opt}\nx := 21\n~~~\n```go\nx * 2\n```\n';
  if (out !== want) fail('the edited notebook downloads as ' + JSON.stringify(out));
  // Ctrl+Shift+Up moves the selected cell up; the file follows
  await page.evaluate((s) => [...document.querySelectorAll(s)].pop().focus(), CODE_CELLS);
  await page.keyboard.press('Control+Shift+ArrowUp');
  await page.waitForFunction((s) => document.querySelector(s).querySelector('textarea').value === 'x * 2', CODE_CELLS);
  const moved = (await downloadBytes(page)).toString();
  if (!moved.includes('```go\nx * 2\n```\n~~~go {.opt}\nx := 21\n~~~\n')) fail('after a move: ' + JSON.stringify(moved));
  await openFile(page, 'again.md', moved);
  await waitDone(page);
  const again = (await downloadBytes(page)).toString();
  console.log('round trip: ' + (again === moved ? 'byte for byte' : 'DIFFERS'));
  if (again !== moved) fail('download, open, download changed the file: ' + JSON.stringify(again));

  // A cell switched to Julia is checked, not run, and the cell that reads it
  // is blocked, as a ```julia cell is; switched back, it is Go again. (x = 21
  // is Julia, and not Go: the text is never translated.)
  await openFile(page, 'jl.md', '```go\nx = 21\n```\n```go\nx * 2\n```\n');
  await waitDone(page);
  await settle(page);
  await page.locator(CODE_CELLS).first().locator('.lang-select').selectOption('julia');
  await waitDone(page);
  await settle(page);
  await page.click('#run');
  await waitDone(page);
  cells = await codeState(page);
  console.log('after Go -> Julia: ' + JSON.stringify(cells.map((c) => [c.lang, c.out.slice(0, 50)])));
  const jbadge = await page.$eval(CODE_BADGES, (b) => b.querySelector('select').selectedOptions[0].text + b.querySelector('.lang-status').textContent);
  if (cells[0].lang !== 'julia' || jbadge !== 'Julia · not runnable yet' || !cells[0].out.includes('not run yet') || !cells[1].out.includes('blocked: it depends on cell 0, a Julia cell'))
    fail('Go -> Julia: ' + jbadge + ' ' + JSON.stringify(cells));
  if ((await downloadBytes(page)).toString() !== '```julia\nx = 21\n```\n```go\nx * 2\n```\n') fail('the Julia fence is not in the file');
  await settle(page);
  await page.locator(CODE_CELLS).first().locator('.lang-select').selectOption('go');
  await waitDone(page);
  if ((await downloadBytes(page)).toString() !== '```go\nx = 21\n```\n```go\nx * 2\n```\n') fail('Julia -> Go did not give the file back');

  // Open refuses a file over 1 MiB, and says so
  await openFile(page, 'big.md', Buffer.alloc((1 << 20) + 1, 0x61));
  await page.waitForSelector('body[data-state="refused"]', { timeout: 5000 }).catch(() => fail('a 1 MiB + 1 file was not refused'));
  const refused = await page.textContent('#status');
  if (!/up to 1 MiB/.test(refused)) fail('the refusal does not say why: ' + refused);
}

// A cell that runs past the time limit is stopped as the kernel stops it,
// and the next run gets a fresh runtime.
async function limitChecks() {
  const page = await browser.newPage();
  page.on('pageerror', (e) => fail('limit page error: ' + e.message));
  await page.goto(new URL('?cell-limit=2', url).href);
  await waitDone(page);
  await openFile(page, 'loop.md', '```go\nx := 1\n```\n```go\nfor {\n}\n```\n');
  await page.waitForSelector(DONE, { timeout: 60000 });
  const cells = await codeState(page);
  console.log('time limit: ' + cells[1].out);
  if (!cells[1].out.includes('stopped: the cell ran longer than 2s, the limit; its worker was killed')) fail('an endless loop was not stopped: ' + JSON.stringify(cells));
  await openFile(page, 'after.md', '```go\n1 + 1\n```\n');
  await waitDone(page);
  const after = await codeState(page);
  if (after[0].out !== '2') fail('the run after the time limit: ' + JSON.stringify(after));
  await page.close();
}

try {
  await carouselChecks();
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

  // A figure cannot run code in the page: whatever SVG a cell shows (a Go
  // value's SaveSVG, a Ruby cell's) loses its scripts, handlers, animations
  // and javascript: links.
  const hostile = [
    '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>',
    '<svg xmlns="http://www.w3.org/2000/svg"><a href="java&#9;script:alert(1)"><text>x</text></a></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><a><set attributeName="href" to="javascript:alert(1)"/><text>x</text></a></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><animate attributeName="onclick" values="alert(1)"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><iframe src="x"/></foreignObject></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><a href=" data:text/html,x"><text>x</text></a></svg>',
    // External references and style sheets: nothing a figure holds may fetch.
    '<svg xmlns="http://www.w3.org/2000/svg"><style>@import url(https://evil.example/x.css)</style></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><image href="https://evil.example/t.png"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="https://evil.example/s.svg#x"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect fill="url(https://evil.example/p#p)" style="filter:url(https://evil.example/f)"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><foreignobject><img xmlns="http://www.w3.org/1999/xhtml" src="https://evil.example/i"/></foreignobject></svg>',
  ];
  for (const h of hostile) {
    const out = await page.evaluate((t) => window.playgroundSanitize(t), h);
    if (/script|onload|onclick|<set|<animate|foreignObject|iframe|data:text|evil|<style|<use/i.test(out || '')) fail('the sanitizer let through: ' + out);
  }
  const internal = await page.evaluate(() =>
    window.playgroundSanitize('<svg xmlns="http://www.w3.org/2000/svg"><g clip-path="url(#c)"><rect/></g></svg>'));
  if (!/clip-path="url\(#c\)"/.test(internal || '')) fail('the sanitizer dropped a reference inside the figure: ' + internal);
  const kept = await page.evaluate(() =>
    window.playgroundSanitize('<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/png;base64,AA=="/></svg>'));
  if (!/data:image\/png/.test(kept || '')) fail('the sanitizer dropped an embedded PNG: ' + kept);

  const names = await page.$$eval('#examples a', (as) => as.map((a) => a.dataset.name));
  let seenRuby = false;
  for (const [i, name] of names.entries()) {
    const ruby = await page.evaluate((n) => {
      const ex = JSON.parse(document.getElementById('examples-data').textContent).find((x) => x.name === n);
      return ex.cells.some((c) => c.lang === 'ruby' || c.lang === 'julia'); // either loads the Ruby core
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
      const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
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
    // Each cell's badge names the language of its fence, in text, for
    // screen readers too, and says nothing more once it can run.
    const badges = await page.evaluate((n) => {
      const ex = JSON.parse(document.getElementById('examples-data').textContent).find((x) => x.name === n);
      const want = ex.cells.filter((c) => c.kind === 'code').map((c) => ({ go: 'Go', ruby: 'Ruby', julia: 'Julia · not runnable yet' })[c.lang || 'go']);
      const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
      return {
        want,
        got: cells.map((c) => {
          const b = c.querySelector('.lang-badge');
          const ta = c.querySelector('textarea');
          // the badge is the language control: its chosen option, then the status
          const sel = b && b.querySelector('select');
          const text = b && (sel ? sel.selectedOptions[0].text + b.querySelector('.lang-status').textContent : b.textContent);
          return b && { text, runnable: b.dataset.runnable, label: ta.getAttribute('aria-label'), described: ta.getAttribute('aria-describedby') === b.id, visible: b.getBoundingClientRect().width > 0 };
        }),
      };
    }, name);
    console.log(`${name}: badges ${badges.got.map((b) => (b ? b.text : 'none')).join(', ')}`);
    badges.want.forEach((w, i) => {
      const b = badges.got[i];
      // A Julia cell is checked, not run: its badge says "not runnable yet", and
      // its accessible name is the language alone.
      const julia = w.startsWith('Julia');
      const lang = julia ? 'Julia' : w;
      if (!b || b.text !== w || b.runnable !== (julia ? 'no' : 'yes') || b.label !== lang + ' cell' || !b.described || !b.visible)
        fail(`${name}: cell ${i} should show the badge "${w}": ${JSON.stringify(b)}`);
    });
    if (badges.want.some((w) => w.startsWith('Julia'))) await juliaExampleChecks(page, name);
    // Download gives the notebook back byte for byte, as the site serves it.
    const file = await (await page.request.get(new URL(`notebooks/${name}.md`, page.url()).href)).body();
    const got = await downloadBytes(page);
    if (!got.equals(file)) fail(`${name}: Download is not the notebook byte for byte (${got.length} bytes, the file ${file.length})`);
    // Running one cell runs the cells above it, never more: not slower than
    // running them all.
    if (i === 0) {
      const one = [];
      const all = [];
      for (let k = 0; k < 3; k++) {
        one.push(await ranMs(page, () => page.locator(CODE_CELLS).first().locator('.actions button', { hasText: 'Run' }).click()));
        all.push(await ranMs(page, () => page.click('#run')));
      }
      const med = (xs) => [...xs].sort((a, b) => a - b)[1];
      console.log(`${name}: run the first cell ${med(one)} ms, run all ${med(all)} ms (medians of 3)`);
      if (med(one) > med(all) * 1.2 + 5) fail(`${name}: running one cell (${med(one)} ms) is slower than running all (${med(all)} ms)`);
    }
  }
  // A notebook a visitor opens runs with no bridge to the page's JavaScript.
  await page.evaluate(() => (document.body.dataset.state = 'switching'));
  await page.setInputFiles('#open', {
    name: 'probe.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# probe\n\n```ruby\n[defined?(JS).inspect, (File.read("/etc/passwd") rescue $!.class.name)]\n```\n'),
  });
  await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 180000 });
  const probe = await page.$eval('#notebook .cell .output', (e) => e.textContent).catch(() => '');
  console.log('probe:', probe);
  if (!/^\["nil", "Errno::/.test(probe)) fail('a Ruby cell reaches the page or the files: ' + probe);

  // A fence in a language that is not a cell's stays prose, and its badge
  // says it does not run; a hostile word is text.
  await page.evaluate(() => (document.body.dataset.state = 'switching'));
  await page.setInputFiles('#open', {
    name: 'langs.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# langs\n\n```python\nprint(1)\n```\n\n```__proto__\nx\n```\n\n```<img/src/onerror=alert(1)>\ny\n```\n\n```go\n1 + 1\n```\n'),
  });
  await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 180000 });
  const langs = await page.evaluate(() => ({
    prose: [...document.querySelectorAll('#notebook .prose .lang-badge')].map((b) => b.textContent),
    cells: [...document.querySelectorAll('#notebook .cell[data-lang] .cell-head select')].map((s) => s.selectedOptions[0].text),
    injected: document.querySelectorAll('#notebook img').length,
  }));
  console.log('other fences: ' + langs.prose.join(' | ') + '; cells: ' + langs.cells.join(' | '));
  if (langs.prose[0] !== 'python · not run: shown as text' || langs.prose[1] !== '__proto__ · not run: shown as text' || langs.prose.length !== 3)
    fail('a fence in another language does not say it is not run: ' + JSON.stringify(langs.prose));
  if (langs.injected) fail('a fence\'s info string became markup');
  if (langs.cells.join() !== 'Go') fail('the Go cell after them: ' + JSON.stringify(langs.cells));

  // A Julia cell is checked, not run: its badge says so, the cell that reads
  // it is blocked, the other runs; the core with the Julia parser is loaded.
  await page.evaluate(() => (document.body.dataset.state = 'switching'));
  const tj = Date.now();
  await page.setInputFiles('#open', {
    name: 'julia.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# julia\n\n```julia\ntotal = sum(1:10)\n```\n\n```go\ntotal + 1\n```\n\n```go\n2 * 21\n```\n'),
  });
  await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 180000 });
  const jl = await page.evaluate(() => {
    const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
    return cells.map((c) => {
      const b = c.querySelector('.cell-head .lang-badge');
      const text = b.querySelector('select').selectedOptions[0].text + b.querySelector('.lang-status').textContent;
      return { badge: text, runnable: b.dataset.runnable, out: c.querySelector('.outputs').textContent };
    });
  });
  console.log(`julia: ${Date.now() - tj} ms; ` + jl.map((c) => `${c.badge} [${c.out}]`).join(' | '));
  if (jl.length !== 3) fail('julia: ' + JSON.stringify(jl));
  else {
    if (jl[0].badge !== 'Julia · not runnable yet' || jl[0].runnable !== 'no' || !jl[0].out.includes('Julia cells are parsed and checked, but not run yet'))
      fail('the Julia cell: ' + JSON.stringify(jl[0]));
    if (!jl[1].out.includes('blocked: it depends on cell 0, a Julia cell')) fail('the cell reading the Julia cell: ' + JSON.stringify(jl[1]));
    if (!jl[2].out.includes('42')) fail('the independent cell: ' + JSON.stringify(jl[2]));
  }

  await editingChecks(page);
  await limitChecks();
} finally {
  await browser.close();
  if (server) server.close();
}
process.exit(failures ? 1 : 0);
