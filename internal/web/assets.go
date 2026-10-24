package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// Static files are linked with a hash of their content (asset), so a new
// release is fetched at once: without it, browsers kept the old styles and
// scripts for up to a day after an update, next to the new pages.

// assetHashes maps "/static/..." paths to a short hash of the file.
var assetHashes = hashStatic()

func hashStatic() map[string]string {
	out := map[string]string{}
	fs.WalkDir(staticFiles, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // the embedded tree has no unreadable entries
		}
		data, err := staticFiles.ReadFile(p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		out["/"+p] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
	return out
}

// asset returns the URL of a static file with its content hash, such as
// /static/css/app.css?v=1a2b3c4d5e.
func asset(path string) string {
	if h, ok := assetHashes[path]; ok {
		return path + "?v=" + h
	}
	return path
}

// cacheStatic sets how long browsers keep a static file: a year when the
// URL carries the content hash (the URL changes with the content), and a
// day otherwise (fonts, which the stylesheet links without a hash).
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.URL.Query().Get("v"); v != "" && v == assetHashes[strings.TrimSuffix(r.URL.Path, "/")] {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
