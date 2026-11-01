// The playground's Go core, in a worker so that a long run never freezes the
// page and Stop (or the time limit) can end it. Messages in: {id, op: "run"
// | "parse" | "edit", markdown, last?, ops?}; out: {type: "ready"},
// {type: "started", id, cell}, {type: "result", id, json, ms},
// {type: "error", id, error}, {type: "fatal", error}, {type: "downloading"},
// {type: "downloaded"}.
'use strict';

importScripts('wasm_exec.js');

// The core: the Go one, or, for a notebook with Ruby cells (worker.js?engine=ruby),
// the one that runs Ruby cells too. The page gives its URL (?core=), which
// carries a hash of the core's bytes (playground.wasm?v=0123456789ab): the same
// URL is always the same core. An old page gives none, and gets the bare name.
const params = new URLSearchParams(self.location.search);
const versioned = /^playground(-ruby)?\.wasm\?v=[0-9a-f]{12}$/.test(params.get('core') || '') ? params.get('core') : '';
const core = versioned || (params.get('engine') === 'ruby' ? 'playground-ruby.wasm' : 'playground.wasm');

self.playgroundReady = () => postMessage({ type: 'ready' });

// The page's loading carousel needs to know two things about the core: that
// its bytes are coming over the network ({type: "downloading"}, which starts
// the kit's 300 ms), and when they have all crossed the wire ({type:
// "downloaded"}). A core the browser already holds never says the first, so
// the carousel never shows for it, however slow this machine is to start or to
// compile: it keys on whether the core is downloaded, not on how long
// anything takes.
//
// A clone of the response is read to its end beside the compile, which keeps
// compiling as the bytes arrive, and leaves the response itself as it was
// (Chrome caches the compiled code of a response it fetched).
function watched(response) {
  if (response.body) {
    const r = response.clone().body.getReader();
    const drain = () => r.read().then((c) => (c.done ? postMessage({ type: 'downloaded' }) : drain()));
    drain().catch(() => {});
  }
  return response;
}

// fetchCore asks the browser's cache first: a copy of a core whose URL names
// its bytes is right whatever its age, so it is used as it is, without
// revalidating. Only a miss is a download. (The probe is refused with a
// TypeError when nothing is cached, and for a core without a hash in its URL
// it is not made: an old copy could be stale.)
async function fetchCore() {
  if (versioned) {
    try {
      const cached = await fetch(core, { cache: 'only-if-cached', mode: 'same-origin' });
      if (cached.ok) {
        postMessage({ type: 'downloaded' });
        return cached;
      }
    } catch (e) {
      // not cached
    }
  }
  postMessage({ type: 'downloading' });
  return fetch(core).then(watched);
}

(async () => {
  const go = new Go();
  let result;
  try {
    result = await WebAssembly.instantiateStreaming(fetchCore(), go.importObject);
  } catch (e) {
    // A server that does not say application/wasm: compile from the bytes.
    postMessage({ type: 'downloading' });
    const bytes = await (await fetch(core)).arrayBuffer();
    postMessage({ type: 'downloaded' });
    result = await WebAssembly.instantiate(bytes, go.importObject);
  }
  go.run(result.instance);
})().catch((e) => postMessage({ type: 'fatal', error: String(e) }));

// The core says when each cell starts, for the page's time limit.
let current = 0;
self.playgroundStarted = (cell) => postMessage({ type: 'started', id: current, cell });

onmessage = (e) => {
  const { id, op, markdown, last, ops } = e.data;
  current = id;
  try {
    const t = performance.now();
    const json =
      op === 'parse'
        ? self.playgroundCells(markdown)
        : op === 'edit'
          ? self.playgroundEdit(markdown, JSON.stringify(ops))
          : self.playgroundRun(markdown, typeof last === 'number' ? last : -1);
    postMessage({ type: 'result', id, json, ms: performance.now() - t });
  } catch (err) {
    postMessage({ type: 'error', id, error: String(err) });
  }
};
