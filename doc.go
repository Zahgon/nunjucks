// Package nunjucks is a Go implementation of the nunjucks template engine (ported from mozilla/nunjucks 3.2.4,
// BSD-2-Clause, see LICENSE). Templates render exactly as with the JavaScript implementation: values follow
// JavaScript semantics (package js), and errors carry the same messages, template paths and positions.
//
// An Environment loads templates through a FileSystemLoader and renders them with Render or RenderString.
// The cmd/nunjucks-render command renders one template from the command line.
package nunjucks
