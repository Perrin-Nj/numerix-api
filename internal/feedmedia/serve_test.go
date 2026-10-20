package feedmedia

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serve(t *testing.T, dir, name string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	f, err := Open(dir, name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	r := Response{File: f, IfNoneMatch: hdr["If-None-Match"], Range: hdr["Range"], IfRange: hdr["If-Range"]}
	rec := httptest.NewRecorder()
	if err := r.Write(rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestServeRangeETagAndHeaders(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("0123456789", 100) // 1000 bytes
	name := "clip-ab12cd34.mp4"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	full := serve(t, dir, name, nil)
	etag := full.Header().Get("ETag")
	cases := []struct {
		name   string
		hdr    map[string]string
		status int
		body   string
		cr     string
	}{
		{"whole file", nil, 200, body, ""},
		{"first bytes", map[string]string{"Range": "bytes=0-9"}, 206, body[:10], "bytes 0-9/1000"},
		{"open ended", map[string]string{"Range": "bytes=990-"}, 206, body[990:], "bytes 990-999/1000"},
		{"suffix", map[string]string{"Range": "bytes=-5"}, 206, body[995:], "bytes 995-999/1000"},
		{"beyond the end", map[string]string{"Range": "bytes=5000-6000"}, 416, "", "bytes */1000"},
		{"not modified", map[string]string{"If-None-Match": etag}, 304, "", ""},
		{"other etag", map[string]string{"If-None-Match": `"nope"`}, 200, body, ""},
		{"range with matching If-Range", map[string]string{"Range": "bytes=0-1", "If-Range": etag}, 206, body[:2], "bytes 0-1/1000"},
		{"range with stale If-Range", map[string]string{"Range": "bytes=0-1", "If-Range": `"old"`}, 200, body, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serve(t, dir, name, c.hdr)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d", rec.Code, c.status)
			}
			if c.status != 416 && c.status != 304 && rec.Body.String() != c.body {
				t.Fatalf("body %q, want %q", rec.Body.String(), c.body)
			}
			if got := rec.Header().Get("Content-Range"); got != c.cr {
				t.Fatalf("Content-Range %q, want %q", got, c.cr)
			}
			if c.status == 416 { // Go drops the entity headers on a 416
				return
			}
			if got := rec.Header().Get("Cache-Control"); got != CacheControl {
				t.Fatalf("Cache-Control %q", got)
			}
			if rec.Header().Get("Accept-Ranges") != "bytes" || rec.Header().Get("ETag") != etag || !strings.Contains(etag, "ab12cd34") {
				t.Fatalf("headers %v", rec.Header())
			}
		})
	}
	if ct := full.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("Content-Type %q", ct)
	}
}

func TestOpenRefusesUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ok-ab12cd34.webp"), []byte("x"), 0o644)
	for _, name := range []string{"../etc/passwd", "a/b.webp", "..%2f.webp", "UPPER.webp", "x.exe", "missing-ab12cd34.webp", "", "ok-ab12cd34.webp/", `a\b.webp`} {
		if f, err := Open(dir, name); err == nil {
			_ = f.Close()
			t.Errorf("%q must not open", name)
		}
	}
	if f, err := Open(dir, "ok-ab12cd34.webp"); err != nil {
		t.Fatal(err)
	} else {
		_ = f.Close()
	}
}
