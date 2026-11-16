// The playground's Go core, in a worker so that a long run never freezes the
// page and Stop (or the time limit) can end it. Messages in: {id, op: "run"
// | "parse" | "edit" | "memory", markdown, last?, ops?}; out: {type: "ready"},
// {type: "started", id, cell}, {type: "result", id, json, ms},
// {type: "error", id, error}, {type: "fatal", error}, {type: "downloading"},
// {type: "downloaded"}, and, for the Julia core, {type: "progress", fraction},
// {type: "starting"} (the kernel boots), {type: "warming"} and {type: "warmed"}.
'use strict';

importScripts('wasm_exec.js');

// The core: the Go one, or, for a notebook with Ruby cells (worker.js?engine=ruby),
// the one that runs Ruby cells too, or, to run a Julia cell (worker.js?engine=julia),
// the one that runs Julia too, which also downloads the Base image its kernel
// boots from. The page gives its URL (?core=), which carries a hash of the
// core's bytes (playground.wasm?v=0123456789ab): the same URL is always the
// same core. An old page gives none, and gets the bare name.
const params = new URLSearchParams(self.location.search);
const julia = params.get('engine') === 'julia';
const versioned = /^playground(-ruby|-julia)?\.wasm\?v=[0-9a-f]{12}$/.test(params.get('core') || '') ? params.get('core') : '';
const core = versioned || (julia ? 'playground-julia.wasm' : params.get('engine') === 'ruby' ? 'playground-ruby.wasm' : 'playground.wasm');

// The Julia image: the gzipped Base image cmd/build wrote (its name carries the
// release and the start of the digest), and the sizes the page was built with.
// The core checks the digest of the bytes it is given before it decodes them,
// so nothing here is trusted: a wrong size is refused early, a wrong content by
// the kernel.
const imageName = /^julia-image-v[0-9.]+-[0-9a-f]{8}\.bin\.gz$/.test(params.get('image') || '') ? params.get('image') : '';
const imageRaw = Math.min(Math.max(parseInt(params.get('imageRaw'), 10) || 0, 0), 64 << 20);
const coreRaw = Math.min(Math.max(parseInt(params.get('coreRaw'), 10) || 0, 0), 256 << 20);
let coreLoaded = 0;
let imageLoaded = 0;
let lastProgress = 0;
// progress tells the page how much of the Julia core and image has arrived, as
// a fraction of the bytes they are when decoded (the core's as the browser hands
// them over, the image's as they come out of the decompression): at most every 100 ms.
function progress(force) {
  if (!julia || !coreRaw || !imageRaw) return;
  const now = performance.now();
  if (!force && now - lastProgress < 100) return;
  lastProgress = now;
  postMessage({ type: 'progress', fraction: Math.min(1, (coreLoaded + imageLoaded) / (coreRaw + imageRaw)) });
}

// playgroundReady is called by the core's main once its functions are defined.
// The Julia core is not ready until its image is here and a kernel is booted
// from it, which takes seconds: the page says so on the cell.
self.playgroundReady = () => {
  if (!julia) {
    postMessage({ type: 'ready' });
    return;
  }
  setTimeout(async () => {
    try {
      await imageReady;
      postMessage({ type: 'starting' });
      const err = self.playgroundJuliaWarm();
      if (err) throw new Error(err);
      postMessage({ type: 'ready' });
    } catch (e) {
      postMessage({ type: 'fatal', error: String(e && e.message ? e.message : e) });
    }
  }, 0);
};

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
    const drain = () =>
      r.read().then((c) => {
        if (c.done) {
          postMessage({ type: 'downloaded' });
          return;
        }
        coreLoaded += c.value.length;
        progress(false);
        return drain();
      });
    drain().catch(() => {});
  }
  return response;
}

// cachedOrFetch asks the browser's cache first: a copy of a file whose URL names
// its bytes is right whatever its age, so it is used as it is, without
// revalidating. Only a miss is a download. (The probe is refused with a
// TypeError when nothing is cached, and for a name without a hash in it it is
// not made: an old copy could be stale.)
async function cachedOrFetch(name, immutable, onMiss) {
  if (immutable) {
    try {
      const cached = await fetch(name, { cache: 'only-if-cached', mode: 'same-origin' });
      if (cached.ok) return { response: cached, hit: true };
    } catch (e) {
      // not cached
    }
  }
  // said before the request, not after its headers: the wait is the download
  if (onMiss) onMiss();
  return { response: await fetch(name), hit: false };
}

async function fetchCore() {
  const { response, hit } = await cachedOrFetch(core, Boolean(versioned), () => postMessage({ type: 'downloading' }));
  if (hit) {
    postMessage({ type: 'downloaded' });
    coreLoaded = coreRaw;
    progress(true);
    return response;
  }
  return watched(response);
}

// fetchImage downloads the gzipped image, decompresses it as it arrives into one
// Uint8Array of the size the page was built with (which is not in this worker's
// Go memory until the core copies it for a load), and leaves it in
// self.playgroundJuliaImage for the core.
async function fetchImage() {
  if (!imageName || !imageRaw) throw new Error('the page gave no Julia image');
  const { response, hit } = await cachedOrFetch(imageName, true);
  if (!response.ok || !response.body) throw new Error(`the Julia image could not be fetched (HTTP ${response.status})`);
  const out = new Uint8Array(imageRaw);
  let n = 0;
  const r = response.body.pipeThrough(new DecompressionStream('gzip')).getReader();
  for (;;) {
    const { done, value } = await r.read();
    if (done) break;
    if (n + value.length > imageRaw) throw new Error('the Julia image is larger than the one this page pins');
    out.set(value, n);
    n += value.length;
    imageLoaded = n;
    progress(false);
  }
  if (n !== imageRaw) throw new Error(`the Julia image is ${n} bytes, not the ${imageRaw} this page pins`);
  self.playgroundJuliaImage = out;
  progress(true);
  return hit;
}

let go;
const imageReady = julia ? fetchImage() : Promise.resolve();
imageReady.catch(() => {}); // reported where it is awaited, once the core runs

(async () => {
  go = new Go();
  let result;
  try {
    result = await WebAssembly.instantiateStreaming(fetchCore(), go.importObject);
  } catch (e) {
    // A server that does not say application/wasm: compile from the bytes.
    postMessage({ type: 'downloading' });
    const bytes = await (await fetch(core)).arrayBuffer();
    postMessage({ type: 'downloaded' });
    coreLoaded = coreRaw;
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
    if (op === 'memory') {
      // What the core holds, for the page's tests: the Go instance's linear
      // memory, and the image the worker keeps for the next kernel.
      const mem = go && go._inst && go._inst.exports.mem ? go._inst.exports.mem.buffer.byteLength : 0;
      const image = self.playgroundJuliaImage ? self.playgroundJuliaImage.length : 0;
      postMessage({ type: 'result', id, json: JSON.stringify({ wasm: mem, image }), ms: 0 });
      return;
    }
    if (op === 'break' && julia) {
      // For the page's test: close the kernel, so that the next run's Reset fails.
      self.playgroundJuliaBreak();
      postMessage({ type: 'result', id, json: '{}', ms: 0 });
      return;
    }
    const json =
      op === 'parse'
        ? self.playgroundCells(markdown)
        : op === 'edit'
          ? self.playgroundEdit(markdown, JSON.stringify(ops))
          : self.playgroundRun(markdown, typeof last === 'number' ? last : -1);
    postMessage({ type: 'result', id, json, ms: performance.now() - t });
    if (julia && op === 'run' && self.playgroundJuliaReplace()) {
      // Each Julia run starts with Kernel.Reset (go-embedded-julia v0.19.0): the
      // kernel is as booted, in milliseconds, and the memory stays flat. Only a
      // Reset that had to load the image again (the undo log overflowed) or
      // failed leaves this instance holding a boot more (about 250 MiB, and a
      // twelfth would run out of the 4 GiB): then the page is asked to replace
      // this worker, a new wasm instance with a fresh kernel, while it is idle.
      postMessage({ type: 'recycle' });
    }
  } catch (err) {
    postMessage({ type: 'error', id, error: String(err) });
  }
};
