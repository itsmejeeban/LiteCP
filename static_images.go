package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// Cache public embedded image assets only; HTML and authenticated API responses
// retain their existing cache policy. Versioned URLs can be reused across visits.
func cachedImageAssets(next http.Handler, assets fs.FS) http.Handler {
	type imageAsset struct {
		data []byte
		etag string
	}
	var cache sync.Map
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext := strings.ToLower(path.Ext(r.URL.Path))
		if (r.Method != "GET" && r.Method != "HEAD") || !strings.HasPrefix(r.URL.Path, "/") || (ext != ".png" && ext != ".svg" && ext != ".ico" && ext != ".webp" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif") {
			next.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		value, ok := cache.Load(name)
		if !ok {
			data, err := fs.ReadFile(assets, name)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			value, _ = cache.LoadOrStore(name, imageAsset{data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))})
		}
		asset := value.(imageAsset)
		w.Header().Set("ETag", asset.etag)
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(asset.data))
	})
}
