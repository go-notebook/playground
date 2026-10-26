// The playground's Go core, in a worker so that a long run never freezes the
// page and Stop can end it. Messages in: {id, op: "run" | "parse",
// markdown}; out: {type: "ready"}, {type: "result", id, json, ms},
// {type: "error", id, error}, {type: "fatal", error}.
'use strict';

importScripts('wasm_exec.js');

// The core: the Go one, or, for a notebook with Ruby cells (worker.js?engine=ruby),
// the one that runs Ruby cells too.
const core = new URLSearchParams(self.location.search).get('engine') === 'ruby' ? 'playground-ruby.wasm' : 'playground.wasm';

self.playgroundReady = () => postMessage({ type: 'ready' });

// The page's loading carousel needs to know when the core has crossed the
// wire ({type: "downloaded"}). A clone of the response is read to its end
// beside the compile, which keeps compiling as the bytes arrive, and leaves
// the response itself as it was (Chrome caches the compiled code of a
// response it fetched).
function watched(response) {
  if (response.body) {
    const r = response.clone().body.getReader();
    const drain = () => r.read().then((c) => (c.done ? postMessage({ type: 'downloaded' }) : drain()));
    drain().catch(() => {});
  }
  return response;
}

(async () => {
  const go = new Go();
  let result;
  try {
    result = await WebAssembly.instantiateStreaming(fetch(core).then(watched), go.importObject);
  } catch (e) {
    // A server that does not say application/wasm: compile from the bytes.
    const bytes = await (await fetch(core)).arrayBuffer();
    postMessage({ type: 'downloaded' });
    result = await WebAssembly.instantiate(bytes, go.importObject);
  }
  go.run(result.instance);
})().catch((e) => postMessage({ type: 'fatal', error: String(e) }));

onmessage = (e) => {
  const { id, op, markdown } = e.data;
  try {
    const t = performance.now();
    const json = op === 'parse' ? self.playgroundCells(markdown) : self.playgroundRun(markdown);
    postMessage({ type: 'result', id, json, ms: performance.now() - t });
  } catch (err) {
    postMessage({ type: 'error', id, error: String(err) });
  }
};
