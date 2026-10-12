// Package web holds the playground's page: HTML, CSS and a few kilobytes of
// JavaScript, embedded so that cmd/build can write them next to the
// WebAssembly core.
//
// The page follows the fleet's N3 doctrine: the DOM is the page's own, made
// by playground.js; the core, in a Web Worker (worker.js), is called once
// per run with the whole notebook and answers with every cell's results.
package web

import "embed"

// Files holds index.html (a text/template), playground.css, playground.js
// and worker.js.
//
//go:embed index.html playground.css playground.js worker.js
var Files embed.FS
