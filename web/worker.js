// The playground's Go core, in a worker so that a long run never freezes the
// page and Stop can end it. Messages in: {id, op: "run" | "parse",
// markdown}; out: {type: "ready"}, {type: "result", id, json, ms},
// {type: "error", id, error}, {type: "fatal", error}.
'use strict';

importScripts('wasm_exec.js');

self.playgroundReady = () => postMessage({ type: 'ready' });

(async () => {
  const go = new Go();
  let result;
  try {
    result = await WebAssembly.instantiateStreaming(fetch('playground.wasm'), go.importObject);
  } catch (e) {
    // A server that does not say application/wasm: compile from the bytes.
    const bytes = await (await fetch('playground.wasm')).arrayBuffer();
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
