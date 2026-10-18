package main

import (
	_ "embed"
	"net/http"
)

// indexHTML is embedded so the binary is fully self-contained and can be
// started from a USB stick without a static/ directory next to it.
//
//go:embed static/index.html
var indexHTML []byte

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}
