// Loads a built playground in headless Chromium, lets it run its first
// example, and checks the page: every code cell got a result, none failed,
// and each figure is an inline <svg>. Then it runs every other example.
// Before that it drives the loading carousel: shown on a cold load, never on
// a cached one, gone when the runtime is ready, worked by the keyboard, and
// still under prefers-reduced-motion.
//
//   node web/test/page.mjs <directory>   serve a built playground locally
//   node web/test/page.mjs --editor <directory>   ... with the code editor
//   node web/test/page.mjs <url>         check a deployed one (it has the editor)
//
// It prints how long the runtime took to start and the first result to
// appear, counted from the start of the page load. It takes no screenshot.
import { chromium } from 'playwright';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import zlib from 'node:zlib';
import { execFileSync } from 'node:child_process';

const withEditor = process.argv.includes('--editor');
// a deployed page (or --editor) has the code editor, and must be driven through
// it: a textarea is then a failure, a broken editor that fell back must not pass
const expectEditor = withEditor || /^https?:/.test(process.argv.slice(2).find((a) => !a.startsWith('--')) || '');
const target = process.argv.slice(2).find((a) => !a.startsWith('--'));
if (!target) {
  console.error('usage: node page.mjs [--editor] <directory | url>');
  process.exit(2);
}

let server = null;
let url = target;
// How many times the local server sent each file: a load served from the
// browser's cache sends nothing.
const served = new Map();
if (!/^https?:/.test(target)) {
  const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.wasm': 'application/wasm', '.md': 'text/markdown', '.svg': 'image/svg+xml' };
  server = http.createServer(async (req, res) => {
    const name = decodeURIComponent(new URL(req.url, 'http://x').pathname);
    const p = path.join(target, name);
    const file = fs.existsSync(p) && fs.statSync(p).isDirectory() ? path.join(p, 'index.html') : p;
    // By default this check drives the page as a <textarea> page: the code
    // editor that replaces the textareas (editor.wasm) is not served here,
    // which is also the page a visitor gets when it fails to load. With
    // --editor it is served, as on a deployed page, and the editing checks
    // drive the editor's text layer. The editor has its own browser test,
    // browsertest/ (chromedp).
    if (!fs.existsSync(file) || (path.basename(name) === 'editor.wasm' && !withEditor)) {
      res.writeHead(404).end();
      return;
    }
    served.set(path.basename(name), (served.get(path.basename(name)) || 0) + 1);
    // Cacheable, as GitHub Pages serves it (max-age=600), so that a second
    // load can come from the cache.
    // worker.js is never cached here, so that every load asks for it and can
    // be made to wait for it (slow.worker).
    const worker = path.basename(name) === 'worker.js';
    if (worker && slow.worker) await new Promise((r) => setTimeout(r, slow.worker));
    res.writeHead(200, { 'Content-Type': types[path.extname(file)] || 'application/octet-stream', 'Cache-Control': worker ? 'no-store' : 'max-age=600' });
    fs.createReadStream(file).pipe(res);
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  url = `http://127.0.0.1:${server.address().port}/`;
}

// Playwright's own Chromium, or else the installed Chrome.
const browser = await chromium.launch().catch(() => chromium.launch({ channel: 'chrome' }));
// Every request any page of this run made for the Julia core or its image, the
// workers' included (Playwright reports a worker's requests on its context).
const juliaRequests = [];
{
  const newPage = browser.newPage.bind(browser);
  browser.newPage = async (...a) => {
    const p = await newPage(...a);
    p.context().on('request', (r) => JULIA_FILES.test(r.url()) && juliaRequests.push(r.url()));
    return p;
  };
}
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
// How long the local server holds worker.js back, to make a machine on which
// the runtime takes longer to start than the kit's 300 ms.
const slow = { worker: 0 };
// A code cell's language badge (prose cells have one too, and so do the
// fences in prose).
const CODE_BADGES = '#notebook .cell[data-lang] .cell-head .lang-badge';
const corePattern = /\/playground(-ruby|-julia)?\.wasm(\?.*)?$/;
// What a page asks of the network for Julia: the third core and its image. The
// default core must never ask for either unless a Julia cell is run (it is
// asserted below, on the requests of the page and of its workers).
const JULIA_FILES = /\/(playground-julia\.wasm|julia-image-[^/]*\.bin\.gz)(\?|$)/;
// What the page says loading Julia costs: "Loading Julia (~28 MB, about 20 s)…".
const JULIA_NOTE = /Loading Julia \(~(\d+) MB, about (\d+) s\)/;
async function holdCore(context) {
  // The worker first asks the browser's cache for the core (only-if-cached,
  // which fails at once when nothing is cached) and only then downloads it:
  // that first request goes through, the download is held back.
  const asked = new Map();
  await context.route(corePattern, async (route) => {
    const u = route.request().url();
    const first = !asked.has(u);
    asked.set(u, true);
    if (!first) await new Promise((r) => setTimeout(r, HOLD));
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
    // (A Julia cell is runnable: it loads Julia when run, which is not the wait for the core.)
    if (!waiting.length || !waiting.every(([r, t]) => (r === 'no' && /· (loading the (Ruby )?runtime…|waits for the Ruby runtime|not runnable yet)$/.test(t)) || (r === 'yes' && /· loads when run \(~\d+ MB\)$/.test(t))))
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

    // 2b. The core in the cache, on a machine where the runtime is slow to
    // start (the local server holds worker.js back 800 ms, past the kit's
    // 300 ms): the carousel must still not show, because the core is not
    // being downloaded. This is what failed on CI after v0.6.0: the carousel
    // keyed on how long the runtime took to say "downloaded", not on whether
    // the core was coming over the network. (Only the local server can slow
    // the worker, so a deployed page skips this.)
    if (server) {
      slow.worker = 800;
      try {
        await page.goto('about:blank');
        const t0 = served.get('playground.wasm') || 0;
        page = await open(context, 'cached, slow start');
        await waitReady(page);
        st = await state(page);
        const took = await page.evaluate(() => Math.round(window.playgroundTimings.runtimeReady));
        const sent = (served.get('playground.wasm') || 0) - t0;
        console.log(`carousel: cached load, runtime ready ${took} ms after navigation (worker held 800 ms), shown ${st.shown}, core sent ${sent} times`);
        if (took < 800) fail(`the slow start was not slow (ready after ${took} ms): the test measures nothing`);
        if (st.shown) fail(`cached load, runtime ready after ${took} ms: the carousel flashed`);
        if (sent !== 0) fail(`cached load, slow start: the core was sent again (${sent})`);
        if (!(await page.evaluate(() => document.getElementById('loading').hidden))) fail('cached load, slow start: #loading is not hidden');
      } finally {
        slow.worker = 0;
      }
    }
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

// ---- The Julia and R examples ---------------------------------------------

// The check-only languages (C), as their examples show them: the fence word, the
// badge's name, the start of the not-run message and the site the prose links.
const CHECKED = {
  C: { lang: 'c', text: 'C cells are not run yet', site: 'https://github.com/go-embedded-c/c', error: /line 2:11: unexpected identifier/ },
};

// checkedExampleChecks reads the open notebook, an example with cells of the
// check-only language label: those cells say they are checked and not run, in
// the neutral style and not as a failure; a Go cell that reads one is blocked,
// also not a failure; a Go cell that does not runs and shows a value; and the
// prose says plainly that the language is checked, not run, and links its
// project.
async function checkedExampleChecks(page, name, label) {
  const { lang, text, site, error } = CHECKED[label];
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
  const own = r.cells.filter((c) => c.lang === lang);
  const blocked = r.cells.filter((c) => c.lang !== lang && c.out.includes('blocked: it depends on cell'));
  const alone = r.cells.filter((c) => c.lang !== lang && c.out !== '' && !c.out.includes('blocked:') && !c.failStyle);
  console.log(`${name}: ${label} example: ${own.length} ${label}, ${blocked.length} blocked, ${alone.length} independent`);
  if (!own.length || own.some((c) => !c.out.includes(text) || !c.notRunStyle || c.failStyle))
    fail(`${name}: a ${label} cell should say it is checked, not run, and not look failed: ` + JSON.stringify(own));
  if (!blocked.length || blocked.some((c) => !c.notRunStyle || c.failStyle))
    fail(`${name}: no Go cell is blocked on the ${label} cell, or it looks failed: ` + JSON.stringify(r.cells));
  if (!alone.length) fail(`${name}: no Go cell runs independently of the ${label} cell: ` + JSON.stringify(r.cells));
  if (!/checked, not run/.test(r.prose) || !r.links.includes(site))
    fail(`${name}: the prose should say ${label} cells are checked, not run, and link ${site}`);
  if (!error.test(r.prose)) fail(`${name}: the prose should show a syntax error caught at its line`);
}

// rExampleChecks reads the open 5-r notebook: R cells run (#29), so each says
// R on its badge, shows what R prints and nothing in the failed or not-run
// style; the warning comes in R's form; variables persist from one R cell to
// the next; the Go cell beside them runs on its own variable; and the prose
// says R cells run and shows R's own error, not a "checked, not run" notice.
async function rExampleChecks(page, name) {
  const r = await page.evaluate(() => {
    const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
    return {
      cells: cells.map((c) => ({
        lang: c.dataset.lang,
        out: (c.querySelector('.outputs').textContent || '').trim(),
        skippedStyle: c.querySelectorAll('.outputs .output.skipped').length,
        failStyle: c.querySelectorAll('.outputs .output.error').length,
      })),
      prose: [...document.querySelectorAll('#notebook .prose')].map((p) => p.textContent).join('\n'),
    };
  });
  const rs = r.cells.filter((c) => c.lang === 'r');
  console.log(`${name}: R example: ${rs.length} R cells: ` + rs.map((c) => JSON.stringify(c.out.slice(0, 40))).join(' | '));
  if (rs.length < 3 || rs.some((c) => c.skippedStyle || c.failStyle)) fail(`${name}: an R cell looks failed or not run: ` + JSON.stringify(rs));
  if (!rs.some((c) => c.out.includes('[1] 385') && c.out.includes('mean of the squares: 38.5')))
    fail(`${name}: the R cell that reads another R cell's variables shows no real output: ` + JSON.stringify(rs));
  if (!rs.some((c) => c.out.includes('[1] Inf') && c.out.includes('Warning message:') && c.out.includes('In ratio(1, 0) : division by zero')))
    fail(`${name}: the R warning is not shown as R gives it: ` + JSON.stringify(rs));
  const go = r.cells.filter((c) => c.lang === 'go');
  if (!go.length || go.some((c) => c.out !== '42' || c.failStyle || c.skippedStyle)) fail(`${name}: the Go cell should run alone and show 42: ` + JSON.stringify(go));
  if (/checked, not run|not run yet|cannot start/.test(r.prose + rs.map((c) => c.out).join(' '))) fail(`${name}: the example still says R is not run`);
  if (!r.prose.includes('Error in sqrt("a") : non-numeric argument to mathematical function')) fail(`${name}: the prose does not show R's own error`);
}

// rLimitChecks: an R cell that runs away is stopped by the evaluator's own
// bounds, with R's message, and the page stays usable: the cells after it say
// they were not run, the worker is not killed (it is not the page's time limit
// that ended it), and the next run, R and Go, works.
async function rLimitChecks() {
  const page = await browser.newPage();
  page.on('pageerror', (e) => fail('R limit page error: ' + e.message));
  await page.goto(url);
  await waitDone(page);
  const cases = [
    ['steps', 'while (TRUE) 0', /evaluation step limit reached/],
    ['memory', 'x <- NULL\nfor (i in 1:1e6) x <- c(x, i)', /memory limit exceeded: more than 536870912 bytes of vectors allocated/],
    ['recursion', 'f <- function(n) f(n + 1)\nf(1)', /evaluation nested too deeply/],
    // heavier frames per evaluation: these overflowed the browser's call stack
    // (a RangeError, the core lost) at a depth of 1500
    ['recursion through sapply', 'g <- function(n) sapply(1, function(i) g(n + 1))\ng(1)', /evaluation nested too deeply/],
    ['recursion through tryCatch', 'h <- function(n) tryCatch(h(n + 1), error = function(e) stop(e))\nh(1)', /evaluation nested too deeply/],
  ];
  for (const [what, src, want] of cases) {
    const t0 = Date.now();
    await openFile(page, `r-${what}.md`, '```r\n' + src + '\n```\n```r\n1 + 1\n```\n');
    await page.waitForSelector(DONE, { timeout: 60000 });
    const cells = await codeState(page);
    console.log(`R ${what} limit (${Date.now() - t0} ms): ` + JSON.stringify(cells[0].out.slice(0, 90)));
    if (!want.test(cells[0].out) || /worker was killed/.test(cells[0].out)) fail(`R ${what}: a runaway cell was not stopped by the evaluator: ` + JSON.stringify(cells));
    if (!/Not run: a cell above failed/.test(cells[1].out)) fail(`R ${what}: the cell after the stopped one should be skipped: ` + JSON.stringify(cells));
    // the page is usable: an R cell and a Go cell run in the next notebook
    await openFile(page, `r-after-${what}.md`, '```r\nxs <- 1:3\nxs * 2\n```\n```go\n1 + 1\n```\n');
    await waitDone(page);
    const after = await codeState(page);
    if (after[0].out !== '[1] 2 4 6' || after[1].out !== '2') fail(`R ${what}: the run after the limit: ` + JSON.stringify(after));
  }
  // Deep recursion and deeply nested DATA used to overflow the browser's call
  // stack (a RangeError) and kill the Go instance: the call-depth bound did not
  // count the native walkers (identical, unlist, print...). Since go-embedded-r
  // v0.17.1 one depth counter bounds them all, so each is an R error naming
  // the stack, the core is NOT restarted, and the next R cell runs in the very
  // same session.
  const stack = /C stack usage|nested too deeply|stack overflow/i;
  const deep = [
    ['data, identical', 'x <- list()\nfor (i in 1:50000) x <- list(x)\nidentical(x, x)'],
    ['data, unlist', 'x <- list()\nfor (i in 1:50000) x <- list(x)\nlength(unlist(x))'],
    ['calls, infinite', 'f <- function(n) f(n + 1)\nf(0)'],
    ['calls, 2000 deep', 'f <- function(n) if (n == 0) 0 else 1 + f(n - 1)\nf(2000)'],
  ];
  for (const [what, src] of deep) {
    const workers = await page.evaluate(() => window.playgroundWorkers);
    await openFile(page, 'r-deep.md', '```r\n' + src + '\n```\n```r\n1 + 1\n```\n');
    await page.waitForSelector(DONE, { timeout: 120000 });
    const cells = await codeState(page);
    console.log(`R deep ${what}: ` + JSON.stringify(cells[0].out.slice(0, 90)));
    if (!stack.test(cells[0].out) || /the runtime failed|worker was killed/.test(cells[0].out))
      fail(`R deep ${what}: not an R error naming the stack: ` + JSON.stringify(cells));
    const restarted = await page.evaluate((w) => window.playgroundWorkers - w, workers);
    if (restarted !== 0) fail(`R deep ${what}: ${restarted} worker starts, the core must not be restarted`);
    // the next R cell of a fresh notebook runs, still on the same worker
    await openFile(page, 'r-after-deep.md', '```r\n1 + 1\n```\n```go\n2 + 2\n```\n');
    await waitDone(page);
    const after = await codeState(page);
    if (after[0].out !== '[1] 2' || after[1].out !== '4') fail(`R deep ${what}: the run after the error: ` + JSON.stringify(after));
    if ((await page.evaluate((w) => window.playgroundWorkers - w, workers)) !== 0) fail(`R deep ${what}: the next run restarted the core`);
  }
  await page.close();
}

// fortranExampleChecks reads the open 7-fortran notebook: Fortran cells run, so
// each says Fortran on its badge, the one that prints shows what the program
// prints, and none is in the failed or not-run style; a variable persists from one Fortran
// cell to the next; the Go cell beside them runs on its own variable; and the
// prose says Fortran cells run.
async function fortranExampleChecks(page, name) {
  const r = await page.evaluate(() => {
    const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
    return {
      cells: cells.map((c) => ({
        lang: c.dataset.lang,
        out: (c.querySelector('.outputs').textContent || '').trim(),
        skippedStyle: c.querySelectorAll('.outputs .output.skipped').length,
        failStyle: c.querySelectorAll('.outputs .output.error').length,
      })),
      prose: [...document.querySelectorAll('#notebook .prose')].map((p) => p.textContent).join('\n'),
    };
  });
  const fs_ = r.cells.filter((c) => c.lang === 'fortran');
  console.log(`${name}: Fortran example: ${fs_.length} cells: ` + fs_.map((c) => JSON.stringify(c.out.slice(0, 40))).join(' | '));
  if (fs_.length < 2 || fs_.some((c) => c.skippedStyle || c.failStyle) || !fs_[fs_.length - 1].out) fail(`${name}: a Fortran cell looks failed, not run or silent: ` + JSON.stringify(fs_));
  if (!fs_.some((c) => /\b385\b/.test(c.out))) fail(`${name}: the Fortran cell that reads another cell's variable shows no real output: ` + JSON.stringify(fs_));
  const go = r.cells.filter((c) => c.lang === 'go');
  if (!go.length || go.some((c) => c.out !== '42' || c.failStyle || c.skippedStyle)) fail(`${name}: the Go cell should run alone and show 42: ` + JSON.stringify(go));
  if (/checked, not run|not run yet|not yet in the browser|cannot start/.test(r.prose + fs_.map((c) => c.out).join(' '))) fail(`${name}: the example still says Fortran is not run`);
}

// fortranLimitChecks: a Fortran cell that runs away is stopped by the
// evaluator's own bounds (its step count, its 10 s) with a message, and the page
// stays usable: the cell after it says it was not run, the worker is not killed
// (the page's 30 s watchdog did not end it), and the next run, Fortran and Go,
// works. A runaway loop must never hang the tab.
async function fortranLimitChecks() {
  const page = await browser.newPage();
  page.on('pageerror', (e) => fail('Fortran limit page error: ' + e.message));
  await page.goto(url);
  await waitDone(page);
  const cases = [
    ['endless DO', 'integer :: i\ni = 0\ndo\n  i = i + 1\nend do', /step|ran longer than/],
    ['endless DO WHILE', 'logical :: t = .true.\ndo while (t)\nend do', /step|ran longer than/],
    ['nested DO, 10^10 iterations', 'integer :: i, j, s\ns = 0\ndo i = 1, 100000\n  do j = 1, 100000\n    s = s + 1\n  end do\nend do', /step|ran longer than/],
  ];
  let base = null;
  for (const [what, src, want] of cases) {
    const t0 = Date.now();
    // The first case loads the second core, which starts its worker; the
    // count is taken after it, and no later run may start another.
    const workers = base === null ? null : await page.evaluate(() => window.playgroundWorkers);
    await openFile(page, `fortran-${what}.md`, '```fortran\n' + src + '\n```\n```fortran\nprint *, 1\n```\n');
    await page.waitForSelector(DONE, { timeout: 60000 });
    const cells = await codeState(page);
    console.log(`Fortran ${what} (${Date.now() - t0} ms): ` + JSON.stringify(cells[0].out.slice(0, 100)));
    if (!want.test(cells[0].out) || /worker was killed/.test(cells[0].out)) fail(`Fortran ${what}: a runaway cell was not stopped by the evaluator: ` + JSON.stringify(cells));
    if (!/Not run: a cell above failed/.test(cells[1].out)) fail(`Fortran ${what}: the cell after the stopped one should be skipped: ` + JSON.stringify(cells));
    if (workers !== null && (await page.evaluate((w) => window.playgroundWorkers - w, workers)) !== 0) fail(`Fortran ${what}: the core was restarted`);
    base = 1;
    await openFile(page, `fortran-after-${what}.md`, '```fortran\ninteger :: k = 6\nprint *, k * 7\n```\n```go\n1 + 1\n```\n');
    await waitDone(page);
    const after = await codeState(page);
    if (!/^42$/.test(after[0].out.trim()) || after[1].out !== '2') fail(`Fortran ${what}: the run after the limit: ` + JSON.stringify(after));
  }
  // Deep recursion and a memory bomb must be the cell's failure, not the core's:
  // a RangeError (the browser's call stack, which the evaluator's 32768 nested
  // calls overran at about 800 to 2500) or 4 GiB of arrays kills the Go instance
  // and restarts the worker. The error is the evaluator's, the worker count does
  // not move, and the next run, in the same session, works.
  const module = 'module m\ncontains\nrecursive function f(k) result(r)\ninteger :: k, r, i, j\nr = 0\nif (k > 0) then\ndo i = 1, 1\ndo j = 1, 1\nif (i == j) then\nselect case (k)\ncase default\nif (k > 0) then\nr = f(k - 1) + 1\nend if\nend select\nend if\nend do\nend do\nend if\nend function\nend module';
  // a frame with 13 DO/IF pairs and a SELECT around the recursive call costs the
  // host's stack about 14 times a plain one: Chromium overflowed it at 150 to 199
  // nested calls, and the call bound weighs a call by that nesting
  let nested = '', nestedEnd = '';
  const decls = [];
  for (let i = 1; i <= 13; i++) {
    decls.push('i' + i);
    nested += `do i${i} = 1, 1\nif (i${i} == 1) then\n`;
    nestedEnd = 'end if\nend do\n' + nestedEnd;
  }
  const heavy = `module m\ncontains\nrecursive function f(k) result(r)\ninteger :: k, r, ${decls.join(', ')}\nr = 0\nif (k > 0) then\n${nested}select case (k)\ncase default\nr = ${'('.repeat(20)}f(k - 1)${')'.repeat(20)} + 1\nend select\n${nestedEnd}end if\nend function\nend module`;
  // keep() makes a local array of 2^25 integers (256 MiB counted); with
  // pointer, a module pointer is left associated with it
  const aliasBomb = (pointer) => '```fortran\nmodule m\ninteger, pointer :: p(:)\ncontains\nsubroutine keep()\ninteger, target :: a(2**25)\n' + (pointer ? 'p => a\n' : '') + 'end subroutine\nend module\n```\n```fortran\nuse m\ncall keep()\n```\n```fortran\ninteger, allocatable :: b(:)\nallocate(b(2**25))\nprint *, size(b)\n```\n```fortran\nprint *, 1\n```\n';
  const grabCases = [1, 2, 3, 4, 5, 6, 7, 8].map((k) => `case (${k})\np${k} => n\n`).join('');
  const scalarFrames = '```fortran\nmodule m\ninteger, pointer :: p1, p2, p3, p4, p5, p6, p7, p8\ncontains\nsubroutine grab(k)\ninteger :: k\ninteger, target :: n\nreal(8) :: big(50000000)\nbig(1) = 1\nn = k\nselect case (k)\n' + grabCases + 'end select\nend subroutine\nend module\n```\n```fortran\nuse m\ninteger :: k\ndo k = 1, 8\ncall grab(k)\nend do\nprint *, p1 + p2 + p3 + p4 + p5 + p6 + p7 + p8\n```\n';
  const hard = [
    ['deep recursion, 13 DO/IF pairs per frame, infinite', '```fortran\n' + heavy + '\n```\n```fortran\nuse m\nprint *, f(100000)\n```\n', /nested calls/, 1],
    ['deep recursion, 13 DO/IF pairs per frame, 45 deep still runs', '```fortran\n' + heavy + '\n```\n```fortran\nuse m\nprint *, f(45)\n```\n', /^45$/, 1, true],
    ['memory bomb, 100000 objects with a 64000-byte array component', '```fortran\ntype :: t\nreal(8) :: x(8000)\nend type\ntype(t) :: a(100000)\n```\n```fortran\nprint *, 1\n```\n', /memory limit exceeded/, 0],
    ['memory bomb, objects with a 1 MiB character component', '```fortran\ntype :: u\ncharacter(len=1048576) :: c\nend type\ntype(u) :: a(2000)\n```\n```fortran\nprint *, 1\n```\n', /memory limit exceeded/, 0],
    ['memory bomb, two 2^25 arrays are 512 MiB together', '```fortran\ninteger, allocatable :: a(:), b(:)\nallocate(a(2**25))\nallocate(b(2**25))\n```\n```fortran\nprint *, 1\n```\n', /memory limit exceeded/, 0],
    // an array constructor's elements are charged as it grows (fortran v0.24.2)
    ['memory bomb, a 2 GiB array constructor', '```fortran\ninteger :: b(1000000), j\ninteger, allocatable :: c(:)\nb = 1\nc = [(b, j = 1, 268)]\n```\n```fortran\nprint *, 1\n```\n', /memory limit exceeded/, 0],
    ['deep recursion, infinite', '```fortran\n' + module + '\n```\n```fortran\nuse m\nprint *, f(100000)\n```\n', /nested calls/, 1],
    ['deep recursion, 150 deep still runs', '```fortran\n' + module + '\n```\n```fortran\nuse m\nprint *, f(150)\n```\n', /^150$/, 1, true],
    ['memory bomb, two 2^27 arrays', '```fortran\ninteger, allocatable :: a(:), b(:)\nallocate(a(2**27))\nallocate(b(2**27))\n```\n```fortran\nprint *, 1\n```\n', /memory limit exceeded/, 0],
    ['memory bomb, one 2^28 array', '```fortran\ninteger, allocatable :: a(:)\nallocate(a(2**28))\n```\n```fortran\nprint *, 1\n```\n', /memory limit exceeded/, 0],
    // a module pointer to a procedure's local array keeps it alive after the
    // return: its 256 MiB stay counted (fortran v0.29.0 moves the charge to the
    // heap; v0.27.0 credited it and let the next 256 MiB through)
    ['memory bomb, a local array kept by a module pointer', aliasBomb(true), /memory limit exceeded/, 2],
    ['the same local array with no pointer to it is credited', aliasBomb(false), /^33554432$/, 2, true],
    // go-notebook's review of v0.29.0: a scalar pointer to a local scalar held
    // the procedure's whole frame, so eight calls with a 400 MB local array,
    // each leaving a module pointer on a local scalar, kept 3.2 GB alive with
    // the counter at 0. Since fortran v0.30.0 the scalar moves into an 8-byte
    // box when its frame returns: each 400 MB is freed and credited, and the
    // eight calls run in the 512 MiB budget without killing the core
    ['eight 400 MB frames, each left with a module pointer on a local scalar', scalarFrames, /^36$/, 1, true],
  ];
  for (const [what, md, want, idx, ok] of hard) {
    const workers = await page.evaluate(() => window.playgroundWorkers);
    const t0 = Date.now();
    await openFile(page, `fortran-hard-${idx}.md`, md);
    await page.waitForSelector(DONE, { timeout: 60000 });
    const cells = await codeState(page);
    const out = cells[idx].out.trim();
    console.log(`Fortran ${what} (${Date.now() - t0} ms): ` + JSON.stringify(out.slice(0, 100)));
    if (!want.test(out) || /runtime failed|RangeError|worker was killed/.test(out)) fail(`Fortran ${what}: not the evaluator's own result: ` + JSON.stringify(cells));
    if (!ok && cells[idx + 1] && !/Not run: a cell above failed/.test(cells[idx + 1].out)) fail(`Fortran ${what}: the cell after the stopped one should be skipped: ` + JSON.stringify(cells));
    const restarted = await page.evaluate((w) => window.playgroundWorkers - w, workers);
    if (restarted !== 0) fail(`Fortran ${what}: ${restarted} worker start(s), the core must not be restarted`);
    await openFile(page, `fortran-after-hard-${idx}.md`, '```fortran\ninteger :: k = 6\nprint *, k * 7\n```\n```go\n1 + 1\n```\n');
    await waitDone(page);
    const after = await codeState(page);
    if (!/^42$/.test(after[0].out.trim()) || after[1].out !== '2') fail(`Fortran ${what}: the run after: ` + JSON.stringify(after));
  }
  await page.close();
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

// What takes a code cell's keystrokes. A code cell is a <textarea> until the
// code editor (editor.wasm) has loaded, then a canvas whose accessible
// surface is webcanvas's: the text layer (a div with role="textbox", named for
// the cell, data-webcanvas-layer) or its a11y mirror (data-webcanvas-mirror),
// which is what the focus lands on when the page focuses the cell (the layer
// after a click). A deployed page has the editor, the local
// build here does not (unless run with --editor); this test drives either, the
// way a keyboard or a screen reader reaches it, and reads the cell's text from
// the textarea the page keeps as its record of the cell (the editor writes
// every change to it).
// (The expression is written out where it is used: it runs in the page.)

// EXPECTED TO FAIL, go-notebook/playground#40: Shift+Enter sent right after
// the first keystroke into a new cell, while the focus is still on the
// editor's a11y mirror, inserts a line break into the cell although the page
// prevented the key. This reproduces it with no wait. It reproduces on most
// runs, so up to 3 are tried; the check passes (XFAIL) when one does, and
// FAILS when none does, which means the bug was fixed (or this no longer
// reaches it): then drop this check and the wait in editingChecks.
async function shiftEnterRace(page) {
  for (let attempt = 1; attempt <= 3; attempt++) {
    await openFile(page, 'race.md', '```go\nx := 1\n```\n');
    await waitDone(page);
    await page.click('#add-end');
    await page.click('.add-menu:not([hidden]) button[data-kind="ruby"]');
    await page.waitForFunction(() => document.activeElement.hasAttribute('data-webcanvas-mirror'), null, { timeout: 10000 });
    await page.keyboard.type('x * 2');
    await page.keyboard.press('Shift+Enter'); // no wait
    await page.waitForTimeout(1000);
    const text = await page.evaluate(() => [...document.querySelectorAll('#notebook .cell[data-lang]')].pop().querySelector('textarea').value);
    if (text === 'x * 2\n') {
      console.log(`XFAIL (#40): Shift+Enter with no wait inserted a line break (attempt ${attempt})`);
      return;
    }
  }
  fail('playground#40 did not reproduce in 3 attempts: Shift+Enter no longer inserts a line break (fixed? remove this expected-to-fail check)');
}

// A visitor adds a Ruby cell, types into it, runs it, makes it Go, deletes
// and brings it back, moves it, and downloads the notebook, which reads back
// byte for byte; all from the + control, the cell's own controls and
// Jupyter's keys.
async function editingChecks(page) {
  const start = '# edit\n\nSome text.\n\n~~~go {.opt}\nx := 21\n~~~\n';
  await openFile(page, 'edit.md', start);
  await waitDone(page);
  // a deployed page, or --editor, has the code editor: say so, so that a page
  // that lost it (or a test that no longer reaches it) is a failure, not a pass
  if (expectEditor) {
    // every code cell on a canvas and none on a text area (the notebook is
    // short: no cell is off screen, where the page shows its textarea)
    const up = await page
      .waitForFunction(
        () => {
          const cells = [...document.querySelectorAll('#notebook .cell[data-lang]')];
          const mounted = window.playgroundEditor ? [...window.playgroundEditor.surfaces].filter((s) => s.mounted).length : 0;
          return cells.length > 0 && mounted >= cells.length && cells.every((c) => c.querySelector('canvas') && !c.querySelector('textarea').offsetParent);
        },
        null,
        { timeout: 120000 }
      )
      .then(() => true, () => false);
    if (!up) fail('the code editor did not load: the cells are still text areas');
    else console.log('code editor: on its canvases, cells edited through its text layer');
  }
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
  if (kinds.join() !== 'Go (as above),Fortran,Julia,R,Ruby,C · not runnable yet,Markdown') fail('the + menu offers ' + kinds.join(', '));
  await page.click('.add-menu:not([hidden]) button[data-kind="ruby"]');
  await page.waitForFunction((ed) => {
    const a = document.activeElement;
    const keys = !!a && (ed ? a.hasAttribute('data-webcanvas-layer') || a.hasAttribute('data-webcanvas-mirror') : a.tagName === 'TEXTAREA');
    return keys && a.closest('.cell').dataset.lang === 'ruby';
  }, expectEditor);
  const mode = await page.evaluate(() => document.activeElement.closest('.cell').querySelector('.mode').textContent);
  if (!/^editing/.test(mode)) fail('a cell being edited does not say so: ' + mode);
  await page.keyboard.type('x * 2');
  await settle(page);
  // The editor's first keystroke hands the focus from its a11y mirror to its
  // text layer, about 100-200 ms later; a Shift+Enter that lands before that
  // inserts a line break (a person does not type that fast; the typed text is
  // not at risk). Wait for the layer, as browsertest/ does.
  await page.waitForFunction((ed) => {
    const a = document.activeElement;
    return !!a && (ed ? a.hasAttribute('data-webcanvas-layer') : a.tagName === 'TEXTAREA');
  }, expectEditor);
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
  await page.waitForFunction(
    ([s, ed]) => {
      const a = document.activeElement;
      const keys = !!a && (ed ? a.hasAttribute('data-webcanvas-layer') || a.hasAttribute('data-webcanvas-mirror') : a.tagName === 'TEXTAREA');
      return document.querySelectorAll(s).length === 3 && keys;
    },
    [CODE_CELLS, expectEditor]
  );
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

  // A cell switched to Julia is not run by the page's own re-run (it would
  // download Julia for an edit): it says it loads when run, and nothing is
  // fetched. Switched back, it is Go again. (x = 21 is Julia, and not Go: the
  // text is never translated.)
  await openFile(page, 'jl.md', '```go\nx = 21\n```\n```go\n2 * 21\n```\n');
  await waitDone(page);
  await settle(page);
  await page.locator(CODE_CELLS).first().locator('.lang-select').selectOption('julia');
  await waitDone(page);
  cells = await codeState(page);
  console.log('after Go -> Julia: ' + JSON.stringify(cells.map((c) => [c.lang, c.out.slice(0, 50)])));
  const jbadge = await page.$eval(CODE_BADGES, (b) => b.querySelector('select').selectedOptions[0].text + b.querySelector('.lang-status').textContent);
  if (cells[0].lang !== 'julia' || !/^Julia · loads when run \(~\d+ MB\)$/.test(jbadge) || !/^Not run yet: Run loads Julia/.test(cells[0].out))
    fail('Go -> Julia: ' + jbadge + ' ' + JSON.stringify(cells));
  if (juliaRequests.length) fail('an edit to Julia fetched the Julia core: ' + juliaRequests.join(' '));
  if ((await downloadBytes(page)).toString() !== '```julia\nx = 21\n```\n```go\n2 * 21\n```\n') fail('the Julia fence is not in the file');
  await settle(page);
  await page.locator(CODE_CELLS).first().locator('.lang-select').selectOption('go');
  await waitDone(page);
  if ((await downloadBytes(page)).toString() !== '```go\nx = 21\n```\n```go\n2 * 21\n```\n') fail('Julia -> Go did not give the file back');

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

// ---- Julia ----------------------------------------------------------------

// The resident memory of every process of the browser this run started, in MB:
// the renderer that holds the page and its worker is in it, with the 20 MB core,
// the image, and the kernel's heap. Only a local run can say it.
function chromiumRssMB() {
  // The browser this process started is its child; every other process of it
  // descends from that one (other sessions' browsers are not counted).
  const rows = execFileSync('ps', ['-axo', 'pid=,ppid=,rss='], { encoding: 'utf8' })
    .split('\n')
    .map((l) => l.trim().split(/\s+/).map(Number))
    .filter((r) => r.length === 3 && !r.some(Number.isNaN));
  const mine = new Set(rows.filter(([, ppid]) => ppid === process.pid).map(([pid]) => pid));
  for (let grew = true; grew; ) {
    grew = false;
    for (const [pid, ppid] of rows) {
      if (!mine.has(pid) && mine.has(ppid)) {
        mine.add(pid);
        grew = true;
      }
    }
  }
  let kb = 0;
  for (const [pid, , rss] of rows) if (mine.has(pid)) kb += rss;
  return kb / 1024;
}

// juliaChecks runs a Julia cell, in a page that has run none: the cell says it
// loads Julia (with the measured size and time), the cell runs and prints, the
// REPL's soft scope gives 55, an exception shows Julia's own text, a cell that
// spins is stopped by the kernel's timeout and not by killing the worker, the
// next run works on the same worker, a Julia variable does not cross to Go
// (and the cell says so), and the memory and the deep recursion are measured.
// Then a page whose Julia image cannot be fetched: its cells say so, and the
// other languages run.
async function juliaChecks(name) {
  const page = await browser.newPage();
  page.on('pageerror', (e) => fail('Julia page error: ' + e.message));
  const ctx = page.context();
  const wire = new Map();
  ctx.on('requestfinished', async (req) => {
    if (!JULIA_FILES.test(req.url())) return;
    const sizes = await req.sizes().catch(() => null);
    wire.set(path.basename(new URL(req.url()).pathname), sizes ? sizes.responseBodySize : 0);
  });
  let peak = chromiumRssMB();
  const before = peak;
  const sampler = setInterval(() => (peak = Math.max(peak, chromiumRssMB())), 500);
  try {
    await page.goto(new URL('#' + name, url).href);
    await waitDone(page);
    if (juliaRequests.length) fail('Julia: fetched before a Julia cell ran: ' + juliaRequests.join(' '));
    const workers0 = await page.evaluate(() => window.playgroundWorkers);

    // 1. Run: the note on the cell, then the cells.
    const t0 = Date.now();
    await settle(page);
    await page.click('#run');
    const note = await page
      .waitForFunction(() => {
        const o = document.querySelector('#notebook .cell[data-lang="julia"] .outputs');
        return o && /Loading Julia/.test(o.textContent) ? o.textContent : null;
      }, null, { timeout: 5000 })
      .then((h) => h.jsonValue())
      .catch(() => null);
    const said = note && note.match(JULIA_NOTE);
    if (!said) fail('Julia: the cell does not say it loads Julia, with its size and time: ' + JSON.stringify(note));
    await waitDone(page);
    const loadMs = Date.now() - t0;
    let cells = await codeState(page);
    console.log(`Julia ${name}: loaded and ran in ${loadMs} ms; cells ` + JSON.stringify(cells.map((c) => c.out.slice(0, 40))));
    const julia = cells.filter((c) => c.lang === 'julia');
    if (!julia.some((c) => c.out === '55')) fail('Julia soft scope: no cell shows 55: ' + JSON.stringify(julia));
    if (!julia.some((c) => c.out.includes('squares: [1, 4, 9]') && c.out.includes('14'))) fail('Julia: a cell does not print and show its value: ' + JSON.stringify(julia));
    if (!julia.some((c) => c.out === 'the sum of the squares is 14')) fail('Julia: a later cell does not read an earlier Julia cell\'s variable: ' + JSON.stringify(julia));
    if (cells.some((c) => c.lang === 'go' && c.out === '20') === false) fail('Julia: the Go cell beside the Julia cells does not run: ' + JSON.stringify(cells));
    const failedStyle = await page.$$eval('#notebook .output.error', (es) => es.map((e) => e.textContent));
    if (failedStyle.length) fail('Julia example: cells failed: ' + failedStyle.join('; '));
    // (the worker is not replaced after a run: each run resets the kernel, so the
    // badge says Julia alone, at once)
    const badgeNow = () => page.$eval('#notebook .cell[data-lang="julia"] .cell-head .lang-badge', (b) => b.querySelector('select').selectedOptions[0].text + b.querySelector('.lang-status').textContent);
    const restarting = await badgeNow();
    if (restarting !== 'Julia') fail('the badge of a Julia cell once Julia runs: ' + JSON.stringify(restarting));
    if ((await page.evaluate(() => window.playgroundRecycles || 0)) !== 0) fail('Julia: the first run replaced the worker');

    // What was downloaded, against what the note says: the third core and the
    // image, as GitHub Pages serves them (gzip of the core, the image as
    // written); the local server sends the core as it is.
    const core = [...wire.keys()].find((k) => k.endsWith('.wasm'));
    const image = [...wire.keys()].find((k) => k.endsWith('.bin.gz'));
    if (!core || !image) fail('Julia: the core and the image were not both fetched: ' + [...wire.keys()].join(' '));
    else if (!/^https?:/.test(target)) {
      const gz = zlib.gzipSync(fs.readFileSync(path.join(target, 'playground-julia.wasm')), { level: 6 }).length;
      const mb = (gz + wire.get(image)) / 1e6;
      console.log(`Julia download: core ${(wire.get(core) / 1e6).toFixed(1)} MB (${(gz / 1e6).toFixed(1)} MB gzipped, as GitHub Pages sends it) + image ${(wire.get(image) / 1e6).toFixed(1)} MB = ${mb.toFixed(1)} MB over the wire; the page says ~${said && said[1]} MB, about ${said && said[2]} s; measured ${(loadMs / 1000).toFixed(1)} s`);
      if (said && Math.abs(mb - +said[1]) > 0.25 * mb) fail(`Julia: the page says ~${said[1]} MB, the download is ${mb.toFixed(1)} MB`);
      // (the time depends on the machine: a note when it is off by a quarter, a
      // failure only when it is more than three times what the page says)
      if (said && (loadMs / 1000 > 1.25 * +said[2] || loadMs / 1000 < 0.5 * +said[2])) console.log(`NOTE Julia: the page says about ${said[2]} s, this machine took ${(loadMs / 1000).toFixed(1)} s`);
      if (said && loadMs / 1000 > 3 * +said[2]) fail(`Julia: the page says about ${said[2]} s, the load took ${(loadMs / 1000).toFixed(1)} s`);
    }
    const mem = await page.evaluate(() => window.playgroundMemory());
    console.log(`Julia memory after the first run: wasm linear memory ${(mem.wasm / 1048576).toFixed(0)} MiB, image held by the worker ${(mem.image / 1048576).toFixed(1)} MiB, browser processes ${chromiumRssMB().toFixed(0)} MB resident (${before.toFixed(0)} MB before, peak ${peak.toFixed(0)} MB)`);

    // 1b. A Julia cell typed in the code editor (the canvas surface, with --editor)
    // or in the textarea: Shift+Enter runs it, in the Julia core already loaded.
    await openFile(page, 'typed.md', '```julia\nx = 20\n```\n');
    await waitDone(page);
    await page.click('#add-end');
    await page.click('.add-menu:not([hidden]) button[data-kind="julia"]');
    await page.waitForFunction((ed) => {
      const a = document.activeElement;
      const keys = !!a && (ed ? a.hasAttribute('data-webcanvas-layer') || a.hasAttribute('data-webcanvas-mirror') : a.tagName === 'TEXTAREA');
      return keys && a.closest('.cell').dataset.lang === 'julia';
    }, expectEditor);
    await page.keyboard.type('6 * 7');
    await settle(page);
    await page.waitForFunction((ed) => {
      const a = document.activeElement;
      return !!a && (ed ? a.hasAttribute('data-webcanvas-layer') : a.tagName === 'TEXTAREA');
    }, expectEditor);
    await page.keyboard.press('Shift+Enter');
    await waitDone(page);
    cells = await codeState(page);
    if (cells.length !== 2 || cells[1].lang !== 'julia' || cells[1].out !== '42') fail('a Julia cell typed in the editor: ' + JSON.stringify(cells));

    // 1c. Julia twice in a row: each run starts with Kernel.Reset, so a struct a
    // notebook defines is defined again without error, the first run's variables
    // are gone, and neither run is lost. (More below: 1f.)
    const twice = '```julia\nstruct P\n    x::Int\nend\nP(21).x * 2\n```\n';
    for (const n of [1, 2, 3]) {
      await openFile(page, `twice${n}.md`, twice);
      await page.waitForSelector(DONE, { timeout: 120000 });
      cells = await codeState(page);
      if (cells.length !== 1 || cells[0].out !== '42') fail(`Julia run ${n} of the same notebook: ` + JSON.stringify(cells));
    }
    await openFile(page, 'gone.md', '```julia\nP\n```\n');
    await page.waitForSelector(DONE, { timeout: 120000 });
    cells = await codeState(page);
    if (!/UndefVarError/.test(cells[0].out)) fail('a variable of an earlier Julia run is still there: ' + JSON.stringify(cells));

    // 1d. Runs with calls in flight. A burst of calls is kept in flight while the
    // run ends and runs follow at once: every one must work, and none replaces the
    // worker (a Reset keeps the instance). Then a kernel that is broken under the
    // worker, so that its next Reset fails: that run works (a kernel is booted
    // anew), and the replacement it owes is made, once, made when idle or forced
    // by the next run, whichever comes first.
    const replaced = () => page.evaluate(() => window.playgroundRecycles || 0);
    const rec0 = await replaced();
    const w1 = await page.evaluate(() => window.playgroundWorkers);
    for (const n of [1, 2, 3]) {
      await page.evaluate(() => {
        window.burst = setInterval(() => {
          for (let i = 0; i < 10; i++) window.playgroundCall('parse', { markdown: '```go\n1\n```\n' }).catch(() => {});
        }, 1);
      });
      await openFile(page, `busy${n}.md`, '```julia\nstruct Q\n    x::Int\nend\nQ(7).x\n```\n');
      await page.waitForSelector(DONE, { timeout: 120000 });
      await page.evaluate(() => clearInterval(window.burst));
      cells = await codeState(page);
      if (cells.length !== 1 || cells[0].out !== '7') fail(`Julia run ${n} with calls in flight: ` + JSON.stringify(cells));
    }
    await new Promise((r) => setTimeout(r, 1500));
    if ((await replaced()) !== rec0) fail(`Julia: ${(await replaced()) - rec0} worker replacements over three normal runs, none expected`);
    // A Reset that fails: the next run still works, and owes exactly one replacement.
    await page.evaluate(() => window.playgroundCall('break', {}));
    await openFile(page, 'broken.md', '```julia\nstruct Q\n    x::Int\nend\nQ(8).x\n```\n');
    await page.waitForSelector(DONE, { timeout: 180000 });
    cells = await codeState(page);
    if (cells.length !== 1 || cells[0].out !== '8') fail('Julia run after a kernel broke under the worker: ' + JSON.stringify(cells));
    await page.waitForFunction((n) => (window.playgroundRecycles || 0) >= n, rec0 + 1, { timeout: 120000 }).catch(() => fail('the Julia worker was not replaced after a Reset that failed'));
    await page.evaluate(() => window.playgroundMemory()); // answers when the new worker is ready
    if ((await replaced()) !== rec0 + 1) fail(`a failed Reset owes one replacement, ${(await replaced()) - rec0} were made`);
    if ((await page.evaluate(() => window.playgroundWorkers)) !== w1) fail('a Julia worker replacement was counted as a crash or a restart');
    console.log('Julia: a Reset that failed gave a correct run and exactly one worker replacement');

    // 1e. A Go-only run after a Julia run does not touch the kernel: no replacement,
    // no new fetch, and as fast as a Go run is.
    const r1 = await replaced();
    const j1 = juliaRequests.length;
    const tg = Date.now();
    await openFile(page, 'go-only.md', '```go\n6 * 7\n```\n');
    await page.waitForSelector(DONE, { timeout: 60000 });
    const goMs = Date.now() - tg;
    cells = await codeState(page);
    await new Promise((r) => setTimeout(r, 3000));
    console.log(`Julia then a Go-only run: ${goMs} ms, replacements ${(await replaced()) - r1}, new Julia fetches ${juliaRequests.length - j1}`);
    if (cells[0].out !== '42' || goMs > 5000) fail(`a Go-only run after Julia: ${goMs} ms ` + JSON.stringify(cells));
    if ((await replaced()) !== r1 || juliaRequests.length !== j1) fail('a Go-only run after a Julia run rebooted Julia');
    await openFile(page, 'after-go.md', '```julia\n1 + 1\n```\n');
    await page.waitForSelector(DONE, { timeout: 120000 });
    cells = await codeState(page);
    if (cells[0].out !== '2') fail('a Julia run after the Go-only run: ' + JSON.stringify(cells));

    // 1f. Thirty runs in one worker: each takes milliseconds after the first (a
    // Reset, not a boot), the wasm memory stays flat, no worker is replaced, and
    // the state really is back each time: a global gone, a const and a struct
    // defined again, and the first draw of rand() the same after a seeded run.
    const rec1 = await replaced();
    const runJulia = async (name, src) => {
      const t = Date.now();
      await openFile(page, name, '```julia\n' + src + '\n```\n');
      await page.waitForSelector(DONE, { timeout: 180000 });
      return { ms: Date.now() - t, cells: await codeState(page) };
    };
    const first = (await runJulia('draw1.md', 'rand()')).cells[0].out;
    const seeded = (await runJulia('draw2.md', 'using Random\nRandom.seed!(7)\nrand()')).cells[0].out;
    const again = (await runJulia('draw3.md', 'rand()')).cells[0].out;
    if (!/^0\.\d+/.test(first) || seeded === first || again !== first) fail(`Julia RNG: first draw ${first}, seeded run ${seeded}, first draw of the next run ${again}`);
    await runJulia('state1.md', 'g1 = 5\nconst K = 1\nstruct S\n    x::Int\nend\nnothing');
    const gone = (await runJulia('state2.md', 'g1')).cells[0].out;
    if (!/UndefVarError/.test(gone)) fail('Julia: a global of the run before is still there: ' + gone);
    const redefined = (await runJulia('state3.md', 'const K = 2\nstruct S\n    y::Int\nend\n(K, fieldnames(S))')).cells[0].out;
    if (redefined !== '(2, (:y,))') fail('Julia: a const and a struct are not defined again: ' + redefined);
    const memSeries = [];
    const msSeries = [];
    for (let n = 1; n <= 30; n++) {
      const r = await runJulia(`thirty${n}.md`, 'struct P\n    x::Int\nend\nP(21).x * 2');
      if (r.cells.length !== 1 || r.cells[0].out !== '42') fail(`Julia run ${n} of 30: ` + JSON.stringify(r.cells));
      msSeries.push(r.ms);
      memSeries.push(Math.round((await page.evaluate(() => window.playgroundMemory())).wasm / 1048576));
    }
    console.log(`Julia 30 runs in one worker: ms ${msSeries.join(' ')}; wasm MiB ${memSeries.join(' ')}; worker replacements ${(await replaced()) - rec1}`);
    if ((await replaced()) !== rec1) fail(`Julia: ${(await replaced()) - rec1} worker replacements over 30 normal runs`);
    // (Not flat to the MiB: the Go heap of the instance grows about 2.5 MiB a run
    // until the collector's pace catches up, and Go-only runs do the same, 1.5 MiB;
    // measured, it stops at about 810 MiB. A boot would be 250 MiB a run: 30 runs
    // of a kernel boot would be 7.5 GiB. The bound is 4 MiB a run.)
    if (Math.max(...memSeries) - memSeries[0] > 120) fail(`Julia: the wasm memory grows over 30 runs: ${memSeries.join(' ')}`);
    if (Math.max(...msSeries) > 3000) fail(`Julia: a run after the first took ${Math.max(...msSeries)} ms, a Reset takes milliseconds: ${msSeries.join(' ')}`);

    // 2. The cells of one run, each on the same worker.
    const run = async (src, extra = '') => {
      await openFile(page, 'j.md', '```julia\n' + src + '\n```\n' + extra);
      await page.waitForSelector(DONE, { timeout: 120000 });
      return codeState(page);
    };
    cells = await run('println("hi")\n1 + 1');
    if (cells[0].out !== 'hi\n2' && cells[0].out !== 'hi2') fail('Julia print and value: ' + JSON.stringify(cells));
    cells = await run('s = 0\nfor i in 1:10\n    s += i\nend\ns');
    if (cells[0].out !== '55') fail('Julia soft scope: ' + JSON.stringify(cells));
    cells = await run('sqrt(-1)', '```go\n1 + 1\n```\n');
    const styled = await page.$$eval('#notebook .output.error', (es) => es.length);
    if (!/^ERROR: DomainError with -1\.0:/.test(cells[0].out) || !styled || !/Not run: a cell above failed/.test(cells[1].out)) fail('Julia exception: ' + JSON.stringify(cells));
    cells = await run('x = (1,');
    if (!/^line 1:8: syntax error/.test(cells[0].out)) fail('Julia syntax error: ' + JSON.stringify(cells));

    // 3. A Julia variable does not reach Go, nor a Go variable Julia, and the cell says so.
    await openFile(page, 'cross.md', '```julia\ntotal = 55\n```\n```go\ntotal + 1\n```\n');
    await page.waitForSelector(DONE, { timeout: 60000 });
    cells = await codeState(page);
    if (!/undefined: total/.test(cells[1].out) || !/Julia variables stay in Julia/.test(cells[1].out)) fail('Julia -> Go does not cross, and does not say so: ' + JSON.stringify(cells));
    await openFile(page, 'cross2.md', '```go\nn := 10\n```\n```julia\nn + 1\n```\n');
    await page.waitForSelector(DONE, { timeout: 60000 });
    cells = await codeState(page);
    if (!/UndefVarError/.test(cells[1].out) || !/does not read the variables of other languages/.test(cells[1].out)) fail('Go -> Julia does not cross, and does not say so: ' + JSON.stringify(cells));

    // 4. A cell that spins is stopped by the kernel at its 10 s, with Julia's
    // message; the worker is not killed (the page's 30 s limit did not fire).
    const workers = await page.evaluate(() => window.playgroundWorkers);
    const ts = Date.now();
    // (an empty `while true` is stopped by whichever bound it reaches first: its
    // 20 million statements come up in about 9 s here, the 10 s next to it; a
    // sleep has no statements to count, so it is the timeout alone)
    const bounds = /^stopped: the Julia cell (ran longer than 20s|executed more than 20000000 statements), the limit$/;
    cells = await run('while true\nend', '```julia\n1 + 1\n```\n');
    console.log(`Julia spinning cell (${Date.now() - ts} ms): ` + JSON.stringify(cells[0].out));
    if (!bounds.test(cells[0].out)) fail('a spinning Julia cell: ' + JSON.stringify(cells));
    const ts2 = Date.now();
    cells = await run('sleep(60)', '```julia\n1 + 1\n```\n');
    const spun = Date.now() - ts2;
    console.log(`Julia sleeping cell (${spun} ms): ` + JSON.stringify(cells[0].out));
    if (cells[0].out !== 'stopped: the Julia cell ran longer than 20s, the limit' || /worker was killed/.test(cells[0].out)) fail('a sleeping Julia cell is not stopped by the kernel\'s timeout: ' + JSON.stringify(cells));
    if (spun > 60000) fail(`the sleeping cell took ${spun} ms: the kernel's 20 s did not end it`);
    if (!/Not run: a cell above failed/.test(cells[1].out)) fail('the cell after the stopped one should be skipped: ' + JSON.stringify(cells));
    if ((await page.evaluate(() => window.playgroundWorkers)) !== workers) fail('a spinning Julia cell restarted the core');
    cells = await run('6 * 7');
    if (cells[0].out !== '42' || (await page.evaluate(() => window.playgroundWorkers)) !== workers) fail('the run after the spinning cell: ' + JSON.stringify(cells));

    // 5. The limits: memory and statements end a cell with a message, on the same worker.
    cells = await run('zeros(200_000_000)');
    console.log('Julia memory limit: ' + JSON.stringify(cells[0].out.slice(0, 100)));
    if (!/^stopped: the Julia cell went over its memory limit/.test(cells[0].out)) fail('Julia memory limit: ' + JSON.stringify(cells));
    cells = await run('n = 0\nfor i in 1:30_000_000\n    n += 1\nend');
    if (!bounds.test(cells[0].out)) fail('Julia step limit: ' + JSON.stringify(cells));
    if ((await page.evaluate(() => window.playgroundWorkers)) !== workers) fail('a limit restarted the Julia core');

    // 6. Deep recursion, in the default V8 stack: measured, not worked around.
    // Whether it ends in a Julia error or loses the core (a RangeError), the
    // outcome is printed; the test fails only when the page is left unusable.
    for (const [what, src] of [
      ['1,000 deep', 'f(n) = n == 0 ? 0 : 1 + f(n - 1)\nf(1_000)'],
      ['10,000 deep', 'f(n) = n == 0 ? 0 : 1 + f(n - 1)\nf(10_000)'],
      ['100,000 deep', 'f(n) = n == 0 ? 0 : 1 + f(n - 1)\nf(100_000)'],
      ['infinite', 'g(n) = g(n + 1) + 1\ng(1)'],
    ]) {
      const w = await page.evaluate(() => window.playgroundWorkers);
      const t = Date.now();
      cells = await run(src);
      const restarts = (await page.evaluate(() => window.playgroundWorkers)) - w;
      console.log(`Julia recursion ${what} (${Date.now() - t} ms, ${restarts} worker restarts): ` + JSON.stringify(cells[0].out.slice(0, 110)));
      // Measured in Chromium: Julia's own depth bound answers (StackOverflowError)
      // from 10,000 deep on; the core is not lost. A loss would be a defect to
      // report upstream, not to work around.
      if (restarts) fail(`deep recursion (${what}) killed the Julia core in Chromium (report to go-embedded-julia): ${cells[0].out}`);
      if (what !== '1,000 deep' && !/^ERROR: StackOverflowError/.test(cells[0].out)) fail(`recursion ${what}: ${JSON.stringify(cells)}`);
      const after = await run('1 + 2');
      if (after[0].out !== '3') fail(`after Julia recursion ${what}, the next run: ` + JSON.stringify(after));
    }
    // Every boot of a kernel keeps memory in the instance until it ends (about
    // 250 MiB), and a Reset keeps none: over the more than fifty runs above the
    // only replacement is the one the broken kernel (1d) owed.
    const recycles = await page.evaluate(() => window.playgroundRecycles || 0);
    const finalMem = await page.evaluate(() => window.playgroundMemory());
    console.log(`Julia: ${recycles} planned worker replacements over the runs above; linear memory now ${(finalMem.wasm / 1048576).toFixed(0)} MiB; browser processes ${chromiumRssMB().toFixed(0)} MB resident, peak ${peak.toFixed(0)} MB (${before.toFixed(0)} MB before Julia)`);
    if (recycles !== 1) fail(`Julia: ${recycles} worker replacements over more than fifty runs, one expected (the broken kernel's)`);
  } finally {
    clearInterval(sampler);
    await page.close();
  }

  // 7. The Julia image cannot be fetched: Julia's cells say so, once, and the
  // other languages run, in that page and in the notebooks after it.
  const bad = await browser.newPage();
  bad.on('pageerror', (e) => fail('Julia failure page error: ' + e.message));
  const attempts = [];
  // (by path: the worker's own URL carries the image's name in its query)
  await bad.route((u) => /\/julia-image-[^/]*\.bin\.gz$/.test(u.pathname), (route) => {
    attempts.push(route.request().url());
    return route.abort('connectionrefused');
  });
  await bad.goto(url);
  await waitDone(bad);
  await openFile(bad, 'down.md', '# down\n\n```go\n1 + 1\n```\n\n```julia\n1 + 1\n```\n\n```go\n2 * 21\n```\n');
  await bad.waitForSelector(DONE, { timeout: 60000 });
  await bad.click('#run');
  await bad.waitForFunction(() => /did not start/.test(document.getElementById('status').textContent), null, { timeout: 60000 }).catch(() => fail('Julia failure: the status does not say Julia did not start'));
  await waitDone(bad);
  let cells = await codeState(bad);
  console.log('Julia that cannot start: ' + JSON.stringify(cells.map((c) => c.out.slice(0, 90))));
  if (cells[0].out !== '2' || !/^Julia did not run: its runtime did not start \(/.test(cells[1].out) || !/The other languages are unaffected/.test(cells[1].out)) fail('Julia that cannot start: ' + JSON.stringify(cells));
  await openFile(bad, 'go.md', '```go\n6 * 7\n```\n');
  await waitDone(bad);
  cells = await codeState(bad);
  if (cells[0].out !== '42') fail('Go after a Julia that cannot start: ' + JSON.stringify(cells));
  // (the worker asks the cache first, then the network: two requests at most, once)
  if (attempts.length < 1 || attempts.length > 2) fail(`the page tried the Julia image ${attempts.length} times`);
  await bad.close();
}

try {
  await carouselChecks();
  const page = await browser.newPage();
  page.on('pageerror', (e) => fail('page error: ' + e.message));
  // The Ruby core must be fetched only once a notebook with a Ruby cell is
  // open, and how much of it crosses the wire is part of the report.
  let rubyFetched = null;
  page.on('requestfinished', async (req) => {
    if (!/\/playground-ruby\.wasm(\?|$)/.test(req.url())) return;
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
  let timed = false; // the one-cell against run-all timing, on the first notebook without Julia
  let sawJuliaExample = null;
  for (const [i, name] of names.entries()) {
    const ruby = await page.evaluate((n) => {
      const ex = JSON.parse(document.getElementById('examples-data').textContent).find((x) => x.name === n);
      const core = new Map(JSON.parse(document.getElementById('languages-data').textContent).map((l) => [l.name, l.core]));
      return ex.cells.some((c) => c.kind === 'code' && core.get(c.lang || 'go') === 'ruby'); // any language but Go loads the Ruby core
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
    // A notebook with a Julia cell is not run by the page itself past the cells
    // above the first one: Julia loads when the reader runs it (juliaChecks).
    const firstJulia = await page.evaluate((n) => {
      const ex = JSON.parse(document.getElementById('examples-data').textContent).find((x) => x.name === n);
      return ex.cells.filter((c) => c.kind === 'code').findIndex((c) => c.lang === 'julia');
    }, name);
    if (firstJulia >= 0) {
      if (juliaRequests.length) fail(`${name}: the Julia core was fetched before a Julia cell ran: ` + juliaRequests.join(' '));
      if (r.withResult !== firstJulia) fail(`${name}: ${r.withResult} cells with a result, the ${firstJulia} above the first Julia cell expected`);
      sawJuliaExample = name;
    } else if (r.withResult !== r.cells) fail(`${name}: ${r.cells - r.withResult} cells without a result`);
    if (r.errors.length) fail(`${name}: ${r.errors.join('; ')}`);
    if (r.figures !== r.svgFigures) fail(`${name}: a figure is not an <svg>`);
    // Each cell's badge names the language of its fence, in text, for
    // screen readers too, and says nothing more once it can run.
    const badges = await page.evaluate((n) => {
      const ex = JSON.parse(document.getElementById('examples-data').textContent).find((x) => x.name === n);
      const langs = new Map(JSON.parse(document.getElementById('languages-data').textContent).map((l) => [l.name, l]));
      const want = ex.cells.filter((c) => c.kind === 'code').map((c) => {
        const l = langs.get(c.lang || 'go');
        if (c.lang === 'julia') return 'JULIA'; // it loads when run: see below
        return l.runs ? l.label : l.label + ' · not runnable yet';
      });
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
      // A Julia or R cell is checked, not run: its badge says "not runnable
      // yet", and its accessible name is the language alone.
      // A Julia cell runs, once its core is loaded, which a run of it does:
      // until then its badge says "loads when run".
      if (w === 'JULIA') {
        if (!b || !/^Julia · loads when run \(~\d+ MB\)$/.test(b.text) || b.runnable !== 'yes' || b.label !== 'Julia cell' || !b.described || !b.visible)
          fail(`${name}: cell ${i} should say Julia loads when run: ${JSON.stringify(b)}`);
        return;
      }
      const checked = w.endsWith(' · not runnable yet');
      const lang = checked ? w.slice(0, -' · not runnable yet'.length) : w;
      if (!b || b.text !== w || b.runnable !== (checked ? 'no' : 'yes') || b.label !== lang + ' cell' || !b.described || !b.visible)
        fail(`${name}: cell ${i} should show the badge "${w}": ${JSON.stringify(b)}`);
    });
    for (const label of Object.keys(CHECKED))
      if (badges.want.includes(label + ' · not runnable yet')) await checkedExampleChecks(page, name, label);
    if (badges.want.includes('R')) await rExampleChecks(page, name);
    if (badges.want.includes('Fortran')) await fortranExampleChecks(page, name);
    // Download gives the notebook back byte for byte, as the site serves it.
    const file = await (await page.request.get(new URL(`notebooks/${name}.md`, page.url()).href)).body();
    const got = await downloadBytes(page);
    if (!got.equals(file)) fail(`${name}: Download is not the notebook byte for byte (${got.length} bytes, the file ${file.length})`);
    // Running one cell runs the cells above it, never more: not slower than
    // running them all.
    // (on a notebook of Go cells alone: another language's cells take milliseconds
    // that vary on a loaded machine, which the Julia example used to absorb)
    if (!timed && firstJulia < 0 && !ruby && !badges.want.some((w) => !/^Go$/.test(w))) {
      timed = true;
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

  // A Julia cell opened in a notebook is not run by the page: its badge says it
  // loads when run, the cells around it that do not read it run when the reader
  // runs the notebook, and nothing of Julia is fetched.
  await page.evaluate(() => (document.body.dataset.state = 'switching'));
  await page.setInputFiles('#open', {
    name: 'julia.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# julia\n\n```go\n1 + 1\n```\n\n```julia\ntotal = sum(1:10)\n```\n\n```go\n2 * 21\n```\n'),
  });
  await page.waitForSelector('body[data-state="done"], body[data-state="error"]', { timeout: 180000 });
  const jl = await codeState(page);
  console.log('julia opened: ' + jl.map((c) => `${c.lang} [${c.out.slice(0, 60)}]`).join(' | '));
  if (jl.length !== 3 || jl[0].out !== '2' || !/^Not run yet: Run loads Julia \(~\d+ MB, about \d+ s\) and runs this cell\.$/.test(jl[1].out) || !/^Not run yet/.test(jl[2].out))
    fail('a Julia cell opened: ' + JSON.stringify(jl));

  await editingChecks(page);
  if (expectEditor) await shiftEnterRace(page);
  await limitChecks();
  await rLimitChecks();
  await fortranLimitChecks();
  // Only now, with every other language run, does a Julia cell run: nothing of
  // Julia was fetched until here.
  if (juliaRequests.length) fail('the Julia core or its image was fetched though no Julia cell ran: ' + juliaRequests.join(' '));
  // (only the notebook site has a Julia example; the others show nothing of Julia)
  if (sawJuliaExample === null) console.log('no example with a Julia cell on this site: the Julia checks are not run');
  else await juliaChecks(sawJuliaExample);
} finally {
  await browser.close();
  if (server) server.close();
}
process.exit(failures ? 1 : 0);
