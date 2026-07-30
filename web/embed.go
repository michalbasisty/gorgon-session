// Package web embeds the dashboard HTML/JS/CSS into the binary so the
// final executable ships with zero external file dependencies.
package web

import "embed"

// Files is the embedded web/ directory. The exported name is used by
// cmd/gorgon/main.go to construct an io/fs.FS for the HTTP file server.
//
//go:embed index.html shared.js summary.js history.js favor-traders.js settings-warcache.js init.js style.css
var Files embed.FS
