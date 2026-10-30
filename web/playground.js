// The playground page. It owns the DOM; the Go core runs in a Web Worker
// (worker.js). The notebook's Markdown is the only source of truth: every
// edit goes to the core, which applies it through go-notebook's format
// package and writes the document back (engine.Edit); a run sends the whole
// document and gets every code cell's results back as JSON.
'use strict';

(() => {
  const $ = (id) => document.getElementById(id);
  const examples = JSON.parse($('examples-data').textContent);
  const article = $('notebook');
  const status = $('status');
  const runButton = $('run');
  const stopButton = $('stop');
  const undoButton = $('undo');

  // The largest notebook the page opens, in bytes (engine.MaxNotebook), and
  // how long one cell may run before its worker is killed, as the kernel's
  // --timeout does.
  const MAX_NOTEBOOK = 1 << 20;
  // ?cell-limit=N (1 to 300 s) changes it, for a test that cannot wait 30 s.
  const limitParam = new URLSearchParams(location.search).get('cell-limit');
  const CELL_LIMIT_S = /^[0-9]{1,3}$/.test(limitParam || '') && +limitParam >= 1 && +limitParam <= 300 ? +limitParam : 30;

  // Timings, in milliseconds since the page started loading
  // (performance.now()), for the status line and for tests.
  const timings = (window.playgroundTimings = { runtimeReady: null, firstResult: null, rubyReady: null, firstRubyResult: null });

  // ---- The loading carousel ---------------------------------------------

  // The landing kit's (cmd/build writes it): it shows only while the core is
  // still downloading after 300 ms, so a cached core never shows it. It is
  // told when the core has crossed the wire, and when the first runtime is
  // ready; it then fades away, or waits with its Start button for a reader
  // who is in it. It covers the first wait only, whichever core that is: a
  // notebook opened later shows its own wait in the status line.
  const carousel = window.landingkitCarousel;
  let carouselDone = false;
  function carouselOver() {
    if (carouselDone || !carousel) return;
    carouselDone = true;
    carousel.done(() => {
      const box = $('loading');
      const inside = box.contains(document.activeElement);
      box.hidden = true;
      if (inside) article.focus(); // focus goes to the notebook, not to the body
    });
  }

  // ---- The worker -------------------------------------------------------

  let worker = null;
  let ready = false;
  // engine is the core the worker runs: 'go', or 'ruby' — the core with
  // Ruby cells too, much larger, loaded only for a notebook that has one.
  let engine = 'go';
  let queue = [];
  const pending = new Map();
  let nextId = 1;
  // The time limit: armed while a run is in the worker, restarted as each
  // cell starts (the core says which), and the worker killed when it fires.
  let watchdog = null;
  let runningCell = -1;
  let overLimit = false;

  function startWorker() {
    ready = false;
    fatal = null;
    updateBadges();
    worker = new Worker(engine === 'ruby' ? 'worker.js?engine=ruby' : 'worker.js');
    worker.onmessage = (e) => {
      const m = e.data;
      if (m.type === 'downloaded') {
        if (carousel) carousel.downloaded();
        return;
      }
      if (m.type === 'ready') {
        ready = true;
        carouselOver();
        if (timings.runtimeReady === null) timings.runtimeReady = performance.now();
        if (engine === 'ruby' && timings.rubyReady === null) timings.rubyReady = performance.now();
        runButton.disabled = false;
        updateBadges();
        for (const q of queue) worker.postMessage(q);
        queue = [];
        return;
      }
      if (m.type === 'fatal') {
        carouselOver(); // the wait is over: the status line says why
        fatal = m.error;
        updateBadges();
        setStatus('The Go runtime did not start: ' + m.error);
        return;
      }
      if (m.type === 'started') {
        runningCell = m.cell;
        arm();
        return;
      }
      const p = pending.get(m.id);
      if (!p) return;
      pending.delete(m.id);
      if (m.type === 'error') p.reject(new Error(m.error));
      else p.resolve(m);
    };
  }

  // call sends {op, ...payload} to the worker, once it is ready.
  function call(op, payload) {
    return new Promise((resolve, reject) => {
      const msg = { id: nextId++, op, ...payload };
      pending.set(msg.id, { resolve, reject });
      if (ready) worker.postMessage(msg);
      else queue.push(msg);
    });
  }

  function arm() {
    clearTimeout(watchdog);
    watchdog = setTimeout(() => {
      overLimit = true;
      stop();
    }, CELL_LIMIT_S * 1000);
  }

  function stop() {
    clearTimeout(watchdog);
    worker.terminate();
    for (const p of pending.values()) p.reject(new Error('stopped'));
    pending.clear();
    queue = [];
    startWorker();
  }

  // needsRubyCore reports whether cells need the second core: it runs Ruby
  // cells, and checks Julia cells (it has go-embedded-julia's parser, which
  // the default core leaves out).
  const needsRubyCore = (cells) => cells.some((c) => c.kind === 'code' && (c.lang === 'ruby' || c.lang === 'julia'));

  // ensureEngine switches to the Ruby core when the cells hold a Ruby or a
  // Julia cell, once: a visitor who runs only Go never downloads it. It
  // reports whether they do.
  function ensureEngine(cells) {
    const needed = needsRubyCore(cells);
    if (needed && engine !== 'ruby') {
      engine = 'ruby';
      worker.terminate();
      for (const p of pending.values()) p.reject(new Error('stopped'));
      pending.clear();
      queue = [];
      startWorker();
      setStatus(cells.some((c) => c.kind === 'code' && c.lang === 'ruby') ? 'Loading the Ruby runtime…' : 'Loading the Julia parser…');
    }
    return needed;
  }

  // ---- Languages --------------------------------------------------------

  // Each cell says, in text, which language runs it: the first word of its
  // fence's info string (```go, ```ruby), as go-notebook's format package
  // reads it, never guessed from the code. While the runtime that runs that
  // language is not ready, or cannot run it, the badge says so too. The
  // badge is the cell's language control: changing it rewrites the fence.
  const LANGS = { go: 'Go', ruby: 'Ruby', julia: 'Julia', markdown: 'Markdown' };
  const langName = (l) => (Object.hasOwn(LANGS, l) ? LANGS[l] : l); // "__proto__" is a word too
  // The kinds a cell can be here: the languages this build knows, and prose.
  // A Julia cell is parsed and checked, not run, and the + menu and the
  // language menu say so, in the badge's own words.
  const KINDS = ['go', 'ruby', 'julia', 'markdown'];
  const kindLabel = (k) => (k === 'julia' ? 'Julia · not runnable yet' : langName(k));
  let fatal = null;
  function langStatus(lang) {
    // Julia cells are parsed and checked, never run: go-embedded-julia has no
    // runtime yet (its milestone M3).
    if (lang === 'julia') return 'not runnable yet';
    if (lang !== 'go' && lang !== 'ruby') return 'not run in this playground';
    if (fatal) return 'cannot run: the runtime did not start';
    if (lang === 'ruby' && engine !== 'ruby') return 'waits for the Ruby runtime';
    if (!ready) return lang === 'ruby' ? 'loading the Ruby runtime…' : 'loading the runtime…';
    return '';
  }
  function updateBadges() {
    if (!nb) return;
    for (const c of nb.cells) {
      if (c.kind !== 'code' || !c.el) continue;
      const s = langStatus(c.lang || 'go');
      c.el.status.textContent = s ? ' · ' + s : '';
      c.el.badge.dataset.runnable = s ? 'no' : 'yes';
    }
  }
  window.playgroundLangName = langName; // for web/test/page.mjs

  // ---- The notebook -----------------------------------------------------

  // nb.markdown is the document; nb.cells are its cells as the format
  // package splits it ({kind, lang, source}, and el, their DOM). Typing
  // changes a cell's source here at once and marks it dirty; the next edit,
  // run or download sends the dirty sources to the core first.
  let nb = null;
  const edited = new Map(); // example name -> its Markdown, kept when switching
  const undo = []; // the Markdown before each delete

  function load(name, markdown, cells, title) {
    nb = { name, example: examples.some((x) => x.name === name) ? name : null, title, markdown, cells, dirty: new Set(), notes: new Map() };
    if (worker) ensureEngine(cells);
    else if (needsRubyCore(cells)) engine = 'ruby';
    undo.length = 0;
    undoButton.hidden = true;
    render();
    for (const a of $('examples').querySelectorAll('a')) {
      if (a.dataset.name === name) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    }
    document.title = (title || name) + ' · ' + document.title.split(' · ').pop();
  }

  async function loadExample(name) {
    const ex = examples.find((x) => x.name === name) || examples[0];
    const kept = edited.get(ex.name);
    if (kept === undefined) {
      load(ex.name, ex.markdown, ex.cells.map((c) => ({ ...c })), ex.title);
      return;
    }
    const reply = await call('parse', { markdown: kept });
    load(ex.name, kept, JSON.parse(reply.json), ex.title);
  }

  // The language a new cell is in: that of the code cell above it, as a
  // single-language notebook would have it, or Go.
  function defaultLang(at) {
    for (let i = at - 1; i >= 0; i--) if (nb.cells[i].kind === 'code') return nb.cells[i].lang || 'go';
    return 'go';
  }
  const codeIndex = (i) => nb.cells.slice(0, i).filter((c) => c.kind === 'code').length;

  // ---- Edits through the core -------------------------------------------

  let chain = Promise.resolve();

  // edit sends the dirty sources, then op (if any), to the core, and takes
  // the document it writes back. It resolves to the core's answer, or null.
  function edit(op) {
    const next = chain.then(async () => {
      const sent = new Map([...nb.dirty].map((i) => [i, nb.cells[i].source]));
      const ops = [...sent].map(([index, source]) => ({ op: 'source', index, source }));
      if (op) ops.push(op);
      if (!ops.length) return null;
      const reply = await call('edit', { markdown: nb.markdown, ops });
      const e = JSON.parse(reply.json);
      if (e.error) {
        setStatus('Not done: ' + e.error);
        return null;
      }
      const same = !op && e.cells.length === nb.cells.length && e.cells.every((c, i) => c.kind === nb.cells[i].kind && c.lang === nb.cells[i].lang);
      nb.markdown = e.markdown;
      if (nb.example) edited.set(nb.example, e.markdown);
      if (same) {
        // Only text changed: keep the editors (and the caret); keep dirty
        // what was typed while the core worked.
        for (const [i, src] of sent) if (nb.cells[i].source === src) nb.dirty.delete(i);
        return e;
      }
      nb.dirty.clear();
      // A cell that is still there, same language and text, keeps what it
      // showed (marked stale until the next run).
      const pool = nb.cells.filter((c) => c.kind === 'code' && c.el);
      nb.cells = e.cells;
      render();
      for (const c of nb.cells) {
        if (c.kind !== 'code') continue;
        const k = pool.findIndex((o) => o.lang === c.lang && o.source === c.source);
        if (k < 0) continue;
        const [old] = pool.splice(k, 1);
        c.el.out.replaceChildren(...old.el.out.childNodes);
        c.el.meta.textContent = old.el.meta.textContent;
        c.el.cell.classList.add('stale');
      }
      return e;
    });
    chain = next.catch(() => null);
    return next.catch((err) => {
      setStatus('Not done: ' + err.message);
      return null;
    });
  }

  // Structural edits: each re-renders and puts the focus where the reader
  // expects it.
  async function insertCell(at, lang, then) {
    const e = await edit({ op: 'insert', index: at, lang });
    if (!e) return;
    setStatus(`Added a ${langName(lang)} cell.`);
    if (then === 'edit') enterEdit(e.focus);
    else focusCell(e.focus);
  }

  async function deleteCell(i) {
    await edit(null); // the sources typed so far are part of what undo restores
    const before = nb.markdown;
    const e = await edit({ op: 'delete', index: i });
    if (!e) return;
    undo.push(before);
    undoButton.hidden = false;
    setStatus('Cell deleted. Undo: Z, or the Undo button.');
    if (nb.cells.length) focusCell(e.focus);
    else $('add-end').focus();
  }

  async function undoDelete() {
    const before = undo.pop();
    if (before === undefined) return;
    undoButton.hidden = undo.length === 0;
    const reply = await call('parse', { markdown: before });
    const cells = JSON.parse(reply.json);
    nb.markdown = before;
    nb.cells = cells;
    nb.dirty.clear();
    if (nb.example) edited.set(nb.example, before);
    render();
    setStatus('Delete undone.');
    focusCell(Math.min(lastDeleted, nb.cells.length - 1));
  }
  let lastDeleted = 0;

  async function moveCell(i, to) {
    if (to < 0 || to >= nb.cells.length) return;
    const e = await edit({ op: 'move', index: i, to });
    if (!e) return;
    setStatus(`Cell moved ${to < i ? 'up' : 'down'}.`);
    focusCell(e.focus);
  }

  async function changeLang(i, lang) {
    const from = nb.cells[i].kind === 'code' ? nb.cells[i].lang || 'go' : 'markdown';
    if (from === lang) return;
    const e = await edit({ op: 'lang', index: i, lang });
    if (!e) {
      render();
      return;
    }
    const note = `Changed from ${langName(from)} to ${langName(lang)}: the text was kept as it was, not translated.`;
    nb.notes.set(e.focus, note);
    render();
    setStatus(note);
    const sel = nb.cells[e.focus] && nb.cells[e.focus].el && nb.cells[e.focus].el.select;
    if (sel) sel.focus();
    // the cell is checked and run again, as the cells above it are
    if (lang === 'markdown') run();
    else run(codeIndex(e.focus));
  }

  // ---- Rendering --------------------------------------------------------

  function render() {
    article.textContent = '';
    nb.cells.forEach((c, i) => {
      article.append(addControl(i));
      article.append(c.kind === 'prose' ? proseCell(c, i) : codeCell(c, i));
    });
    article.append(addControl(nb.cells.length, 'add-end'));
    updateBadges();
  }

  // The "+ Add cell" control before cell at (or at the end): it opens the
  // choice of what the new cell is, the language of the cell above first.
  function addControl(at, id) {
    const wrap = document.createElement('div');
    wrap.className = 'add';
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'add-btn';
    if (id) btn.id = id;
    btn.textContent = '+ Add cell';
    btn.setAttribute('aria-label', at === nb.cells.length ? 'Add a cell at the end' : `Add a cell before cell ${at + 1}`);
    btn.setAttribute('aria-expanded', 'false');
    const menu = document.createElement('div');
    menu.className = 'add-menu';
    menu.setAttribute('role', 'group');
    menu.setAttribute('aria-label', 'Kind of the new cell');
    menu.hidden = true;
    const first = defaultLang(at);
    for (const k of [first, ...KINDS.filter((k) => k !== first)]) {
      const b = document.createElement('button');
      b.type = 'button';
      b.dataset.kind = k;
      b.textContent = kindLabel(k) + (k === first ? ' (as above)' : '');
      b.addEventListener('click', () => insertCell(at, k, 'edit'));
      menu.append(b);
    }
    btn.addEventListener('click', () => {
      const open = menu.hidden;
      menu.hidden = !open;
      btn.setAttribute('aria-expanded', String(open));
      if (open) menu.firstChild.focus();
    });
    menu.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        menu.hidden = true;
        btn.setAttribute('aria-expanded', 'false');
        btn.focus();
      }
    });
    wrap.append(btn, menu);
    return wrap;
  }

  // The head every cell has: the language control and its status, the mode
  // the cell is in, as text, and its actions, behind "⋯" when narrow.
  function cellHead(c, i, kind) {
    const head = document.createElement('div');
    head.className = 'cell-head';
    const badge = document.createElement('span');
    badge.className = 'lang-badge';
    badge.id = 'cell-lang-' + i;
    const select = document.createElement('select');
    select.className = 'lang-select';
    select.setAttribute('aria-label', `Language of cell ${i + 1}`);
    // the current language by its name (its status follows), the others as
    // the + menu offers them
    for (const k of KINDS) select.add(new Option(k === kind ? langName(k) : kindLabel(k), k, false, k === kind));
    if (!KINDS.includes(kind)) select.add(new Option(langName(kind), kind, true, true));
    select.addEventListener('change', () => changeLang(i, select.value));
    const st = document.createElement('span');
    st.className = 'lang-status';
    badge.append(select, st);
    const mode = document.createElement('span');
    mode.className = 'mode';
    const more = document.createElement('button');
    more.type = 'button';
    more.className = 'more';
    more.textContent = '⋯';
    more.setAttribute('aria-label', `Actions of cell ${i + 1}`);
    more.setAttribute('aria-expanded', 'false');
    const actions = document.createElement('div');
    actions.className = 'actions';
    actions.id = 'cell-actions-' + i;
    more.setAttribute('aria-controls', actions.id);
    more.addEventListener('click', () => {
      const open = !actions.classList.contains('open');
      actions.classList.toggle('open', open);
      more.setAttribute('aria-expanded', String(open));
    });
    const button = (text, label, f) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = text;
      b.setAttribute('aria-label', label);
      b.title = label;
      b.addEventListener('click', f);
      actions.append(b);
      return b;
    };
    head.append(badge, mode, more, actions);
    const note = nb.notes.get(i);
    let noteEl = null;
    if (note) {
      noteEl = document.createElement('p');
      noteEl.className = 'cell-note';
      noteEl.textContent = note;
    }
    return { head, badge, select, status: st, mode, button, noteEl };
  }

  function sectionFor(c, i, label) {
    const cell = document.createElement('section');
    cell.className = 'cell';
    cell.tabIndex = 0;
    cell.dataset.index = i;
    cell.setAttribute('aria-label', label);
    cell.addEventListener('keydown', (e) => commandKey(e, i));
    cell.addEventListener('focusin', () => modeText());
    cell.addEventListener('focusout', () => setTimeout(modeText));
    return cell;
  }

  function codeCell(c, i) {
    const lang = c.lang || 'go';
    const cell = sectionFor(c, i, `Cell ${i + 1}, ${langName(lang)}`);
    cell.dataset.lang = lang;
    const h = cellHead(c, i, lang);
    h.button('▶ Run', `Run cell ${i + 1} (and the cells above it)`, () => run(codeIndex(i)));
    h.button('↑', `Move cell ${i + 1} up`, () => moveCell(i, i - 1));
    h.button('↓', `Move cell ${i + 1} down`, () => moveCell(i, i + 1));
    h.button('Delete', `Delete cell ${i + 1}`, () => deleteCell((lastDeleted = i)));
    const ta = document.createElement('textarea');
    ta.value = c.source.replace(/\n$/, '');
    ta.spellcheck = false;
    ta.setAttribute('autocapitalize', 'off');
    ta.setAttribute('aria-label', langName(lang) + ' cell');
    ta.setAttribute('aria-describedby', h.badge.id);
    ta.rows = Math.max(1, ta.value.split('\n').length);
    ta.addEventListener('input', () => {
      c.source = ta.value === '' ? '' : ta.value + '\n';
      nb.dirty.add(i);
      ta.rows = Math.max(1, ta.value.split('\n').length);
      cell.classList.add('stale');
    });
    ta.addEventListener('keydown', (e) => editKey(e, i, ta));
    const meta = document.createElement('div');
    meta.className = 'meta';
    const out = document.createElement('div');
    out.className = 'outputs';
    cell.append(h.head);
    if (h.noteEl) cell.append(h.noteEl);
    cell.append(ta, meta, out);
    c.el = { cell, meta, out, badge: h.badge, status: h.status, select: h.select, mode: h.mode, editor: ta };
    return cell;
  }

  function proseCell(c, i) {
    const cell = sectionFor(c, i, `Cell ${i + 1}, Markdown`);
    cell.classList.add('prose-cell');
    const h = cellHead(c, i, 'markdown');
    const view = document.createElement('div');
    view.className = 'prose';
    view.innerHTML = markdown(c.source);
    const ta = document.createElement('textarea');
    ta.hidden = true;
    ta.spellcheck = true;
    ta.value = c.source.replace(/\n$/, '');
    ta.setAttribute('aria-label', 'Markdown cell');
    ta.rows = Math.max(2, ta.value.split('\n').length);
    const toggle = h.button('Edit', `Edit cell ${i + 1}`, () => (ta.hidden ? openProse() : closeProse()));
    h.button('↑', `Move cell ${i + 1} up`, () => moveCell(i, i - 1));
    h.button('↓', `Move cell ${i + 1} down`, () => moveCell(i, i + 1));
    h.button('Delete', `Delete cell ${i + 1}`, () => deleteCell((lastDeleted = i)));
    function openProse() {
      ta.hidden = false;
      view.hidden = true;
      toggle.textContent = 'Done';
      toggle.setAttribute('aria-label', `Show cell ${i + 1} as text`);
      ta.focus();
    }
    async function closeProse() {
      ta.hidden = true;
      view.hidden = false;
      toggle.textContent = 'Edit';
      toggle.setAttribute('aria-label', `Edit cell ${i + 1}`);
      // a prose edit may make or remove a cell: the core splits it again
      if (nb.dirty.has(i)) {
        const e = await edit(null);
        if (e) focusCell(Math.min(i, nb.cells.length - 1));
      } else cell.focus();
    }
    ta.addEventListener('input', () => {
      c.source = ta.value === '' ? '' : ta.value + '\n';
      nb.dirty.add(i);
      ta.rows = Math.max(2, ta.value.split('\n').length);
      view.innerHTML = markdown(c.source);
    });
    ta.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' || (e.key === 'Enter' && (e.shiftKey || e.ctrlKey || e.metaKey))) {
        e.preventDefault();
        closeProse();
      }
    });
    cell.append(h.head);
    if (h.noteEl) cell.append(h.noteEl);
    cell.append(view, ta);
    c.el = { cell, badge: h.badge, select: h.select, mode: h.mode, editor: ta, openProse };
    return cell;
  }

  function focusCell(i) {
    const c = nb.cells[i];
    if (c && c.el) c.el.cell.focus();
  }

  function enterEdit(i) {
    const c = nb.cells[i];
    if (!c || !c.el) return;
    if (c.kind === 'prose') c.el.openProse();
    else c.el.editor.focus();
  }

  // The mode, in text beside the language: "editing" while the caret is in
  // a cell, "selected" while the cell itself has the focus (and a solid or a
  // dashed outline says the same in shape).
  function modeText() {
    if (!nb) return;
    for (const c of nb.cells) {
      if (!c.el) continue;
      const a = document.activeElement;
      c.el.mode.textContent =
        a === c.el.editor ? 'editing · Esc for cell commands' : a === c.el.cell ? 'selected · Enter to edit, A/B to add, D D to delete' : '';
    }
  }

  // ---- Keys: Jupyter's, with the same meanings --------------------------

  // In a cell's editor (edit mode).
  function editKey(e, i, ta) {
    if (e.key === 'Enter' && e.shiftKey) {
      e.preventDefault();
      run(codeIndex(i));
      const next = nb.cells[i + 1];
      if (next && next.el) next.el.cell.focus();
      else $('add-end').focus();
    } else if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      run(codeIndex(i));
    } else if (e.key === 'Enter' && e.altKey) {
      e.preventDefault();
      run(codeIndex(i));
      insertCell(i + 1, defaultLang(i + 1), 'edit');
    } else if (e.key === 'Escape') {
      e.preventDefault();
      nb.cells[i].el.cell.focus();
    } else if (e.key === 'Tab' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
      // Tab indents, as in an editor; Esc, then Tab, leaves the cell
      e.preventDefault();
      ta.setRangeText('\t', ta.selectionStart, ta.selectionEnd, 'end');
      ta.dispatchEvent(new Event('input'));
    }
  }

  // On a selected cell (command mode).
  let lastD = 0;
  function commandKey(e, i) {
    if (e.target !== e.currentTarget || e.altKey || e.metaKey) return;
    const k = e.key;
    const move = e.ctrlKey && e.shiftKey;
    if (k === 'Enter' && e.shiftKey) {
      if (nb.cells[i].kind === 'code') run(codeIndex(i));
      focusCell(Math.min(nb.cells.length - 1, i + 1));
    } else if (k === 'Enter') enterEdit(i);
    else if (move && k === 'ArrowUp') moveCell(i, i - 1);
    else if (move && k === 'ArrowDown') moveCell(i, i + 1);
    else if (e.ctrlKey) return;
    else if (k === 'ArrowUp' || k === 'k') focusCell(Math.max(0, i - 1));
    else if (k === 'ArrowDown' || k === 'j') focusCell(Math.min(nb.cells.length - 1, i + 1));
    else if (k === 'a' || k === 'A') insertCell(i, defaultLang(i), 'edit');
    else if (k === 'b' || k === 'B') insertCell(i + 1, defaultLang(i + 1), 'edit');
    else if (k === 'm' || k === 'M') changeLang(i, 'markdown');
    else if (k === 'y' || k === 'Y') changeLang(i, defaultLang(i));
    else if (k === 'z' || k === 'Z') undoDelete();
    else if (k === 'd' || k === 'D') {
      const now = Date.now();
      if (now - lastD < 800) {
        lastD = 0;
        deleteCell((lastDeleted = i));
      } else lastD = now;
    } else return;
    e.preventDefault();
  }

  // ---- Running ----------------------------------------------------------

  let running = false;

  // run runs the notebook, or its code cells up to the code cell last (and
  // the cells above it: the playground keeps no values between runs and has
  // no dependency graph). Cells below last keep their results, marked stale.
  async function run(last) {
    if (running) return;
    running = true;
    document.body.dataset.state = 'running';
    runButton.disabled = true;
    const showStop = setTimeout(() => (stopButton.hidden = false), 400);
    setStatus(ready ? 'Running…' : 'Waiting for the Go runtime…');
    try {
      await edit(null);
      const code = nb.cells.filter((c) => c.kind === 'code');
      const upTo = last === undefined || last < 0 ? code.length - 1 : Math.min(last, code.length - 1);
      for (const c of code) c.el.cell.classList.add('stale');
      const hasRuby = ensureEngine(code.slice(0, upTo + 1));
      runningCell = -1;
      overLimit = false; // the limit is armed as each cell starts ('started')
      const reply = await call('run', { markdown: nb.markdown, last: upTo });
      clearTimeout(watchdog);
      const results = JSON.parse(reply.json);
      results.forEach((r, i) => code[i] && show(code[i].el, r));
      if (timings.firstResult === null) timings.firstResult = performance.now();
      if (hasRuby && timings.firstRubyResult === null) timings.firstRubyResult = performance.now();
      const failed = results.find((r) => r.error && !r.status);
      setStatus(
        (failed ? 'A cell failed. ' : '') +
          `Ran ${results.length} cell${results.length === 1 ? '' : 's'} in ${fmtMs(reply.ms)}.` +
          ` Runtime ready ${fmtMs(timings.runtimeReady)}, first result ${fmtMs(timings.firstResult)} after the page started loading.`
      );
      document.body.dataset.state = 'done';
    } catch (err) {
      clearTimeout(watchdog);
      if (overLimit) {
        // The kernel's words for its --timeout.
        const code = nb.cells.filter((c) => c.kind === 'code');
        const c = code[runningCell];
        if (c) {
          c.el.cell.classList.remove('stale');
          c.el.out.textContent = '';
          c.el.out.append(pre(`stopped: the cell ran longer than ${CELL_LIMIT_S}s, the limit; its worker was killed`, 'error'));
        }
        setStatus(`Cell ${runningCell + 1} ran longer than ${CELL_LIMIT_S} s and was stopped. The runtime restarts.`);
      } else setStatus(err.message === 'stopped' ? 'Stopped. The runtime restarts; run again when ready.' : 'Error: ' + err.message);
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
    // A Julia cell that is checked and not run, and a cell blocked on one,
    // are not failures: the engine says so in status, in a neutral style.
    if (r.error) el.out.append(pre(r.error, r.status === 'notrun' || r.status === 'blocked' ? 'skipped' : 'error'));
  }

  // svgNode parses a figure, without scripts, event handlers, animations,
  // style sheets or references outside the document, so that nothing a cell
  // shows can run code in the page or fetch anything: a cell's figure is
  // data, even when the notebook came from someone else. The engine has
  // already rewritten it through an allowlist (engine.SanitizeSVG, fuzzed);
  // this is the second layer, on the DOM the page will insert.
  // A URL scheme is read as a browser reads it, without the tabs, newlines
  // and other control characters it ignores ("java\tscript:" is javascript:).
  const SVG_NS = 'http://www.w3.org/2000/svg';
  const svgDropped = new Set(['script', 'style', 'foreignobject', 'iframe', 'embed', 'object', 'use', 'set', 'animate',
    'animatemotion', 'animatetransform', 'animatecolor', 'discard', 'handler', 'listener', 'feimage']);
  function svgNode(text) {
    const doc = new DOMParser().parseFromString(text, 'image/svg+xml');
    const svg = doc.documentElement;
    if (!svg || svg.namespaceURI !== SVG_NS || svg.localName !== 'svg' || doc.querySelector('parsererror')) return null;
    for (const n of [...svg.querySelectorAll('*')]) {
      if (n.namespaceURI !== SVG_NS || svgDropped.has(n.localName.toLowerCase())) n.remove();
    }
    for (const n of [svg, ...svg.querySelectorAll('*')]) {
      for (const a of [...n.attributes]) {
        const value = a.value.replace(/[\u0000- \u007f]/g, '');
        const raster = n.localName === 'image' && /^data:image\/(png|jpeg|gif);/i.test(value);
        const ref = a.localName === 'href' || a.localName === 'src';
        if (
          /^on/i.test(a.localName) ||
          (ref && !value.startsWith('#') && !raster) ||
          (/^(javascript|vbscript|data):/i.test(value) && !raster) ||
          /url\((?!['"]?#)|\\|image-set|@import/i.test(value)
        )
          n.removeAttribute(a.name);
      }
    }
    if (!svg.getAttribute('viewBox') && svg.getAttribute('width') && svg.getAttribute('height')) {
      svg.setAttribute('viewBox', `0 0 ${parseFloat(svg.getAttribute('width'))} ${parseFloat(svg.getAttribute('height'))}`);
    }
    return document.importNode(svg, true);
  }
  window.playgroundSanitize = (text) => {
    const n = svgNode(text);
    return n ? n.outerHTML : null;
  }; // for web/test/page.mjs

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
        // a fence in a language that is not a cell's stays prose, and says so
        const word = l.slice(m[0].length).trim().split(/\s+/)[0];
        if (word)
          html += `<span class="lang-badge" data-runnable="no"><span class="lang-name">${esc(langName(word))}</span><span class="lang-status"> · not run: shown as text</span></span>`;
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
  window.addEventListener('hashchange', async () => {
    await edit(null); // the example being left keeps its edits
    await loadExample(decodeURIComponent(location.hash.slice(1)));
    run();
  });
  runButton.addEventListener('click', () => run());
  stopButton.addEventListener('click', stop);
  undoButton.addEventListener('click', undoDelete);
  $('reset').addEventListener('click', async () => {
    const ex = examples.find((x) => x.name === nb.example);
    if (!ex) return;
    await edit(null);
    edited.delete(ex.name);
    await loadExample(ex.name);
    run();
  });
  $('download').addEventListener('click', async () => {
    await edit(null); // the file is the document as the core wrote it
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([nb.markdown], { type: 'text/markdown' }));
    a.download = nb.name.replace(/\.md$/, '') + '.md';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  });
  $('open').addEventListener('change', async (e) => {
    const file = e.target.files[0];
    e.target.value = '';
    if (!file) return;
    // An opened notebook is untrusted: bounded in size, run under the same
    // time limit, its figures through both sanitizers.
    if (file.size > MAX_NOTEBOOK) {
      setStatus(`${file.name} is ${(file.size / 1e6).toFixed(1)} MB: the playground opens notebooks up to ${MAX_NOTEBOOK / (1 << 20)} MiB. Nothing was opened.`);
      document.body.dataset.state = 'refused';
      return;
    }
    const text = await file.text();
    setStatus(ready ? 'Reading ' + file.name + '…' : 'Waiting for the Go runtime…');
    const reply = await call('parse', { markdown: text });
    load(file.name, text, JSON.parse(reply.json), file.name);
    run();
  });

  // The first notebook decides which core the worker starts with.
  loadExample(decodeURIComponent(location.hash.slice(1)));
  startWorker();
  run();
})();
