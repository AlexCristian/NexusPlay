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

// [assetHashes] maps each static file's URL path, such as
// "/static/css/app.css", to the first 10 hex digits of its SHA-256 hash.
// Static files are linked with this hash (see [asset]), so a new release is
// fetched at once: without it, browsers kept the old styles and scripts for
// up to a day after an update, next to the new pages.
var assetHashes = hashStatic()

// [hashStatic] hashes every embedded static file, for [assetHashes].
//
// Returns:
//   - [map[string]string]: each file's URL path mapped to the first 10 hex
//     digits of its SHA-256 hash.
func hashStatic() map[string]string {
	hashes := map[string]string{}
	fs.WalkDir(staticFiles, "static", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil // the embedded tree has no unreadable entries
		}
		data, err := staticFiles.ReadFile(path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		hashes["/"+path] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
	return hashes
}

// [asset] gives the URL to link a static file with, carrying its content
// hash, such as "/static/css/app.css?v=1a2b3c4d5e".
//
// Parameters:
//   - [path]: the file's URL path, such as "/static/css/app.css".
//
// Returns:
//   - [string]: [path] with "?v=" and its hash, or [path] unchanged if it is
//     not an embedded static file.
func asset(path string) string {
	if hash, found := assetHashes[path]; found {
		return path + "?v=" + hash
	}
	return path
}

// [cacheStatic] sets how long browsers keep a static file: a year when the
// URL carries the file's current hash (the URL changes with the content),
// and a day otherwise (fonts, which the stylesheet links without a hash).
//
// Parameters:
//   - [next]: the handler that serves the files.
//
// Returns:
//   - [http.Handler]: [next] with the Cache-Control header set.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if version := r.URL.Query().Get("v"); version != "" && version == assetHashes[strings.TrimSuffix(r.URL.Path, "/")] {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
