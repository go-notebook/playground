// The editors' highlighter, in a worker of its own: go-rouge compiled to
// WebAssembly (cmd/highlight), nothing else, so that colouring what is typed
// is never queued behind a notebook that is running in worker.js. Messages
// in: {id, lexer, text}; out: {type: "ready"}, {type: "result", id, html}
// ("" means plain text), {type: "fatal", error}. The page asks for the
// latest text of an editor only, and drops an answer that is no longer for
// what the editor holds.
'use strict';

importScripts('wasm_exec.js');

// The page gives the URL (?core=), which carries a hash of the bytes
// (highlight.wasm?v=0123456789ab): the same URL is always the same file.
const params = new URLSearchParams(self.location.search);
const core = /^highlight\.wasm\?v=[0-9a-f]{12}$/.test(params.get('core') || '') ? params.get('core') : 'highlight.wasm';

self.playgroundReady = () => postMessage({ type: 'ready' });

(async () => {
  const go = new Go();
  let result;
  try {
    result = await WebAssembly.instantiateStreaming(fetch(core), go.importObject);
  } catch (e) {
    // A server that does not say application/wasm: compile from the bytes.
    const bytes = await (await fetch(core)).arrayBuffer();
    result = await WebAssembly.instantiate(bytes, go.importObject);
  }
  go.run(result.instance);
})().catch((e) => postMessage({ type: 'fatal', error: String(e) }));

onmessage = (e) => {
  const { id, lexer, text } = e.data;
  try {
    const t = performance.now();
    const html = self.playgroundHighlight(lexer, text);
    postMessage({ type: 'result', id, html, ms: performance.now() - t });
  } catch (err) {
    postMessage({ type: 'result', id, html: '' });
  }
};
