// The playground page. It owns the DOM; the Go core runs in a Web Worker
// (worker.js) and is called once per run, with the whole notebook as
// Markdown, answering with every code cell's results as JSON.
'use strict';

(() => {
  const $ = (id) => document.getElementById(id);
  const examples = JSON.parse($('examples-data').textContent);
  const article = $('notebook');
  const status = $('status');
  const runButton = $('run');
  const stopButton = $('stop');

  // Timings, in milliseconds since the page started loading
  // (performance.now()), for the status line and for tests.
  const timings = (window.playgroundTimings = { runtimeReady: null, firstResult: null, rubyReady: null, firstRubyResult: null });

  // ---- The worker -------------------------------------------------------

  let worker = null;
  let ready = false;
  // engine is the core the worker runs: 'go', or 'ruby' — the core with
  // Ruby cells too, much larger, loaded only for a notebook that has one.
  let engine = 'go';
  let queue = [];
  const pending = new Map();
  let nextId = 1;

  function startWorker() {
    ready = false;
    worker = new Worker(engine === 'ruby' ? 'worker.js?engine=ruby' : 'worker.js');
    worker.onmessage = (e) => {
      const m = e.data;
      if (m.type === 'ready') {
        ready = true;
        if (timings.runtimeReady === null) timings.runtimeReady = performance.now();
        if (engine === 'ruby' && timings.rubyReady === null) timings.rubyReady = performance.now();
        runButton.disabled = false;
        for (const q of queue) worker.postMessage(q);
        queue = [];
        return;
      }
      if (m.type === 'fatal') {
        setStatus('The Go runtime did not start: ' + m.error);
        return;
      }
      const p = pending.get(m.id);
      if (!p) return;
      pending.delete(m.id);
      if (m.type === 'error') p.reject(new Error(m.error));
      else p.resolve(m);
    };
  }

  function call(op, markdown) {
    return new Promise((resolve, reject) => {
      const msg = { id: nextId++, op, markdown };
      pending.set(msg.id, { resolve, reject });
      if (ready) worker.postMessage(msg);
      else queue.push(msg);
    });
  }

  function stop() {
    worker.terminate();
    for (const p of pending.values()) p.reject(new Error('stopped'));
    pending.clear();
    queue = [];
    startWorker();
  }

  // ensureEngine switches to the Ruby core when the cells hold a Ruby cell,
  // once: a visitor who runs only Go never downloads it. It reports whether
  // they do.
  function ensureEngine(cells) {
    const hasRuby = cells.some((c) => c.kind === 'code' && c.lang === 'ruby');
    if (hasRuby && engine !== 'ruby') {
      engine = 'ruby';
      worker.terminate();
      for (const p of pending.values()) p.reject(new Error('stopped'));
      pending.clear();
      queue = [];
      startWorker();
      setStatus('Loading the Ruby runtime…');
    }
    return hasRuby;
  }

  // ---- The notebook -----------------------------------------------------

  // nb.cells: {kind: "prose" | "code", source}; the DOM of a code cell is
  // nb.cells[i].el (its textarea and its outputs).
  let nb = null;
  const edited = new Map(); // example name -> cells, kept when switching

  function load(name, cells, title) {
    if (nb && nb.example) edited.set(nb.example, nb.cells.map((c) => ({ kind: c.kind, lang: c.lang, source: c.source })));
    nb = { name, example: examples.some((x) => x.name === name) ? name : null, title, cells };
    if (worker) ensureEngine(cells);
    else if (cells.some((c) => c.kind === 'code' && c.lang === 'ruby')) engine = 'ruby';
    render();
    for (const a of $('examples').querySelectorAll('a')) {
      if (a.dataset.name === name) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    }
    document.title = (title || name) + ' · ' + document.title.split(' · ').pop();
  }

  function loadExample(name) {
    const ex = examples.find((x) => x.name === name) || examples[0];
    const cells = edited.get(ex.name) || ex.cells.map((c) => ({ kind: c.kind, lang: c.lang, source: c.source }));
    load(ex.name, cells, ex.title);
  }

  function render() {
    article.textContent = '';
    for (const c of nb.cells) {
      if (c.kind === 'prose') {
        const div = document.createElement('div');
        div.className = 'prose';
        div.innerHTML = markdown(c.source);
        article.append(div);
        continue;
      }
      const cell = document.createElement('section');
      cell.className = 'cell';
      const ta = document.createElement('textarea');
      ta.value = c.source.replace(/\n$/, '');
      ta.spellcheck = false;
      ta.setAttribute('autocapitalize', 'off');
      ta.setAttribute('aria-label', (c.lang === 'ruby' ? 'Ruby' : 'Go') + ' cell');
      cell.dataset.lang = c.lang || 'go';
      ta.rows = Math.max(1, ta.value.split('\n').length);
      ta.addEventListener('input', () => {
        c.source = ta.value + '\n';
        ta.rows = Math.max(1, ta.value.split('\n').length);
        cell.classList.add('stale');
      });
      ta.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
          e.preventDefault();
          run();
        } else if (e.key === 'Tab' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
          e.preventDefault();
          ta.setRangeText('\t', ta.selectionStart, ta.selectionEnd, 'end');
          ta.dispatchEvent(new Event('input'));
        }
      });
      const meta = document.createElement('div');
      meta.className = 'meta';
      const out = document.createElement('div');
      out.className = 'outputs';
      cell.append(ta, meta, out);
      article.append(cell);
      c.el = { cell, meta, out };
    }
  }

  // compose writes the notebook back as Markdown: prose as it was, each code
  // cell in a fence longer than any run of backticks in it.
  function compose() {
    return nb.cells
      .map((c) => {
        if (c.kind === 'prose') return c.source;
        const runs = c.source.match(/`+/g) || [];
        const fence = '`'.repeat(Math.max(3, ...runs.map((r) => r.length + 1)));
        const src = c.source === '' || c.source.endsWith('\n') ? c.source : c.source + '\n';
        return fence + (c.lang || 'go') + '\n' + src + fence + '\n';
      })
      .join('');
  }

  // ---- Running ----------------------------------------------------------

  let running = false;

  async function run() {
    if (running) return;
    running = true;
    document.body.dataset.state = 'running';
    runButton.disabled = true;
    const showStop = setTimeout(() => (stopButton.hidden = false), 400);
    setStatus(ready ? 'Running…' : 'Waiting for the Go runtime…');
    const code = nb.cells.filter((c) => c.kind === 'code');
    for (const c of code) c.el.cell.classList.add('stale');
    const hasRuby = ensureEngine(code);
    try {
      const reply = await call('run', compose());
      const results = JSON.parse(reply.json);
      results.forEach((r, i) => code[i] && show(code[i].el, r));
      if (timings.firstResult === null) timings.firstResult = performance.now();
      if (hasRuby && timings.firstRubyResult === null) timings.firstRubyResult = performance.now();
      const failed = results.find((r) => r.error);
      setStatus(
        (failed ? 'A cell failed. ' : '') +
          `Ran ${results.length} cells in ${fmtMs(reply.ms)}.` +
          ` Runtime ready ${fmtMs(timings.runtimeReady)}, first result ${fmtMs(timings.firstResult)} after the page started loading.`
      );
      document.body.dataset.state = 'done';
    } catch (err) {
      setStatus(err.message === 'stopped' ? 'Stopped. The runtime restarts; run again when ready.' : 'Error: ' + err.message);
      document.body.dataset.state = 'error';
    } finally {
      clearTimeout(showStop);
      stopButton.hidden = true;
      runButton.disabled = !ready;
      running = false;
    }
  }

  function show(el, r) {
    el.cell.classList.remove('stale');
    el.out.textContent = '';
    el.meta.textContent = r.skipped ? '' : fmtMs(r.ms);
    if (r.skipped) {
      el.out.append(pre('Not run: a cell above failed.', 'skipped'));
      return;
    }
    if (r.output) el.out.append(pre(r.output));
    for (const d of r.display || []) {
      if (d['image/svg+xml']) {
        const svg = svgNode(d['image/svg+xml']);
        if (svg) {
          const fig = document.createElement('div');
          fig.className = 'figure';
          fig.append(svg);
          el.out.append(fig);
          continue;
        }
      }
      if (d['image/png']) {
        const fig = document.createElement('div');
        fig.className = 'figure';
        const img = document.createElement('img');
        img.src = 'data:image/png;base64,' + d['image/png'];
        img.alt = d['text/plain'] || '';
        fig.append(img);
        el.out.append(fig);
        continue;
      }
      el.out.append(pre(d['text/plain']));
    }
    if (r.error) el.out.append(pre(r.error, 'error'));
  }

  // svgNode parses a figure, without scripts or event handlers, so that a
  // label cannot run code in the page.
  function svgNode(text) {
    const doc = new DOMParser().parseFromString(text, 'image/svg+xml');
    const svg = doc.documentElement;
    if (!svg || svg.nodeName !== 'svg') return null;
    for (const n of svg.querySelectorAll('script, foreignObject')) n.remove();
    for (const n of [svg, ...svg.querySelectorAll('*')]) {
      for (const a of [...n.attributes]) {
        if (/^on/i.test(a.name) || /^\s*javascript:/i.test(a.value)) n.removeAttribute(a.name);
      }
    }
    if (!svg.getAttribute('viewBox') && svg.getAttribute('width') && svg.getAttribute('height')) {
      svg.setAttribute('viewBox', `0 0 ${parseFloat(svg.getAttribute('width'))} ${parseFloat(svg.getAttribute('height'))}`);
    }
    return document.importNode(svg, true);
  }

  function pre(text, cls) {
    const p = document.createElement('pre');
    p.className = 'output' + (cls ? ' ' + cls : '');
    p.textContent = text;
    return p;
  }

  function fmtMs(ms) {
    if (ms === null || ms === undefined) return '…';
    return ms < 1000 ? `${Math.round(ms * 10) / 10} ms` : `${(ms / 1000).toFixed(2)} s`;
  }

  function setStatus(s) {
    status.textContent = s;
  }

  // ---- Markdown for the prose cells -------------------------------------

  // A small renderer: headings, paragraphs, lists, fenced code, inline
  // code, emphasis, links, and $math$ kept as text.
  function markdown(src) {
    const lines = src.replace(/\r\n/g, '\n').split('\n');
    let html = '';
    let para = [];
    let list = null;
    const flush = () => {
      if (para.length) html += '<p>' + inline(para.join('\n')) + '</p>';
      para = [];
    };
    const closeList = () => {
      if (list) html += `</${list}>`;
      list = null;
    };
    for (let i = 0; i < lines.length; i++) {
      const l = lines[i];
      let m;
      if ((m = l.match(/^ {0,3}(`{3,}|~{3,})/))) {
        flush();
        closeList();
        const body = [];
        for (i++; i < lines.length && !lines[i].trim().startsWith(m[1]); i++) body.push(lines[i]);
        html += '<pre><code>' + esc(body.join('\n')) + '</code></pre>';
      } else if (l.trim() === '$$') {
        flush();
        closeList();
        const body = [];
        for (i++; i < lines.length && lines[i].trim() !== '$$'; i++) body.push(lines[i]);
        html += '<p class="math">' + esc(body.join(' ')) + '</p>';
      } else if ((m = l.match(/^(#{1,6})\s+(.*?)\s*#*\s*$/))) {
        flush();
        closeList();
        const n = Math.min(6, m[1].length + 1);
        html += `<h${n}>${inline(m[2])}</h${n}>`;
      } else if ((m = l.match(/^\s{0,3}([-*+]|\d+[.)])\s+(.*)$/))) {
        flush();
        const kind = /\d/.test(m[1]) ? 'ol' : 'ul';
        if (list !== kind) {
          closeList();
          html += `<${kind}>`;
          list = kind;
        }
        html += '<li>' + inline(m[2]) + '</li>';
      } else if (l.trim() === '') {
        flush();
        closeList();
      } else {
        if (list) closeList();
        para.push(l);
      }
    }
    flush();
    closeList();
    return html;
  }

  function inline(s) {
    // Code spans first, so that nothing inside them is touched.
    return s
      .split(/(`+[^`]*?`+)/)
      .map((part, i) => {
        if (i % 2) return '<code>' + esc(part.replace(/^`+\s?|\s?`+$/g, '')) + '</code>';
        return esc(part)
          .replace(/\$([^$\n]+?)\$/g, '<span class="math">$1</span>')
          .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
          .replace(/(^|[^\w*])\*([^*\s][^*]*?)\*(?!\w)/g, '$1<em>$2</em>')
          .replace(/(^|[^\w])_([^_\s][^_]*?)_(?!\w)/g, '$1<em>$2</em>')
          .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (all, text, url) =>
            /^(https?:\/\/|\/|#|\.{0,2}\/?[\w-])/.test(url) && !/^\s*javascript:/i.test(url)
              ? `<a href="${url}">${text}</a>`
              : all
          );
      })
      .join('');
  }

  function esc(s) {
    return s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
  }

  // ---- Wiring -----------------------------------------------------------

  const nav = $('examples');
  for (const ex of examples) {
    const a = document.createElement('a');
    a.href = '#' + ex.name;
    a.dataset.name = ex.name;
    a.textContent = ex.title;
    nav.append(a);
  }
  window.addEventListener('hashchange', () => {
    loadExample(decodeURIComponent(location.hash.slice(1)));
    run();
  });
  runButton.addEventListener('click', run);
  stopButton.addEventListener('click', stop);
  $('reset').addEventListener('click', () => {
    const ex = examples.find((x) => x.name === nb.example);
    if (!ex) return;
    edited.delete(ex.name);
    nb = null;
    loadExample(ex.name);
    run();
  });
  $('download').addEventListener('click', () => {
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([compose()], { type: 'text/markdown' }));
    a.download = nb.name.replace(/\.md$/, '') + '.md';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  });
  $('open').addEventListener('change', async (e) => {
    const file = e.target.files[0];
    e.target.value = '';
    if (!file) return;
    const text = await file.text();
    setStatus(ready ? 'Reading ' + file.name + '…' : 'Waiting for the Go runtime…');
    const reply = await call('parse', text);
    load(file.name, JSON.parse(reply.json), file.name);
    run();
  });

  // The first notebook decides which core the worker starts with.
  loadExample(decodeURIComponent(location.hash.slice(1)));
  startWorker();
  run();
})();
