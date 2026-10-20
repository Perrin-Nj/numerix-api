package feedmedia

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotFound means no such media file (never reveals why: bad name and missing file look the same).
var ErrNotFound = errors.New("feedmedia: not found")

// contentTypes maps the allowed extensions to their media type. Set by hand: Go reads the system mime table,
// which on a bare Linux image knows neither mp4 nor webm nor vtt.
var contentTypes = map[string]string{
	".webp": "image/webp", ".jpg": "image/jpeg", ".mp4": "video/mp4", ".webm": "video/webm", ".vtt": "text/vtt; charset=utf-8",
}

// hashRe finds the 8 hex digits of the content hash that every file name carries before its extension.
var hashRe = regexp.MustCompile(`-([0-9a-f]{8})\.[a-z0-9]+$`)

// CacheControl: the file name contains the content hash, so the bytes behind a URL never change.
const CacheControl = "public, max-age=31536000, immutable"

// File is an opened media file ready to be served.
type File struct {
	f    *os.File
	Size int64
	Mod  time.Time
	Type string
	ETag string // strong, quoted
	Name string
}

// Open validates name against the allowed pattern (no path separators, no "..") and opens it inside dir.
func Open(dir, name string) (*File, error) {
	if !fileRe.MatchString(name) || strings.Contains(name, "..") {
		return nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil, ErrNotFound
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		_ = f.Close()
		return nil, ErrNotFound
	}
	etag := fmt.Sprintf(`"%x-%x"`, st.Size(), st.ModTime().UnixNano()) // fallback for a name without a hash
	if m := hashRe.FindStringSubmatch(name); m != nil {
		etag = `"` + m[1] + fmt.Sprintf("-%x", st.Size()) + `"`
	}
	return &File{f: f, Size: st.Size(), Mod: st.ModTime(), Type: contentTypes[strings.ToLower(filepath.Ext(name))], ETag: etag, Name: name}, nil
}

// Close releases the file.
func (f *File) Close() error { return f.f.Close() }

// Response is a generated-server response object (Spring: a ResponseEntity) that writes the file with full HTTP
// semantics: ETag and If-None-Match (304), Range and If-Range (206, 416), Accept-Ranges, immutable caching.
// net/http's ServeContent does the Range work; it needs a request, so the three relevant headers are copied into one.
type Response struct {
	File                        *File
	IfNoneMatch, Range, IfRange string
}

// Write sends the file. It closes the file when done.
func (r Response) Write(w http.ResponseWriter) error {
	defer r.File.Close()
	h := w.Header()
	h.Set("ETag", r.File.ETag)
	h.Set("Cache-Control", CacheControl)
	h.Set("Content-Type", r.File.Type)
	h.Set("Accept-Ranges", "bytes")
	h.Set("X-Content-Type-Options", "nosniff")
	req, _ := http.NewRequest(http.MethodGet, "/", nil) // only carries headers into ServeContent
	for k, v := range map[string]string{"If-None-Match": r.IfNoneMatch, "Range": r.Range, "If-Range": r.IfRange} {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	var rs io.ReadSeeker = r.File.f
	http.ServeContent(w, req, r.File.Name, r.File.Mod, rs)
	return nil
}
