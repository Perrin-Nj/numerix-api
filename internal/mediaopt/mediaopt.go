// Package mediaopt shrinks the media files of the app (videos, photos, audio) with ffmpeg, once per file.
//
// Go for Spring people: this is like a @Scheduled job that runs at start-up. It keeps a small JSON manifest
// next to the media ("a done list"), so a file that was already shrunk (or that cannot shrink more) is never
// processed again, and a new file dropped into the folder is picked up on the next start.
package mediaopt

import (
	"bytes"         // looks for the "ANIM" marker of animated WebP files
	"context"       // cancellation (Spring: the shutdown signal)
	"crypto/sha256" // fingerprint of a file, to know whether it changed
	"encoding/hex"  // sha256 bytes as text
	"encoding/json" // the manifest file
	"errors"        // errors.Is
	"fmt"           // error messages
	"io"            // copying a file into the hash
	"io/fs"         // directory walking types
	"log/slog"      // structured logs
	"os"            // files
	"os/exec"       // runs ffmpeg
	"path/filepath" // file names
	"strings"       // extensions
)

// Profile names the settings below. Raise it when the settings change, and every file is shrunk again.
const Profile = "v3"

// manifestName is the done list, stored inside the media folder.
const manifestName = ".optimized.json"

// Runner runs ffmpeg (Spring: an interface so tests can use a fake instead of the real program).
type Runner interface {
	Run(ctx context.Context, args ...string) error
}

// execRunner is the real Runner: it starts the ffmpeg program.
type execRunner struct{ bin string }

// Run starts ffmpeg quietly, overwriting its output file, and returns its error text on failure.
func (r execRunner) Run(ctx context.Context, args ...string) error {
	full := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...) // never wait for a key, never print noise
	var stderr bytes.Buffer                                                                   // keeps ffmpeg's complaint
	cmd := exec.CommandContext(ctx, r.bin, full...)                                           // killed when ctx ends
	if nice, err := exec.LookPath("nice"); err == nil {                                       // lowest CPU priority: the API must stay fast while videos are shrunk
		cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "19", r.bin}, full...)...)
	}
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Optimizer shrinks every media file of one folder.
type Optimizer struct {
	Dir     string       // the media folder (MEDIA_DIR)
	Runner  Runner       // ffmpeg
	Log     *slog.Logger // where progress goes
	MinGain float64      // a result is kept only if it is at least this much smaller (0.10 = 10 percent)
}

// New builds an Optimizer that uses the ffmpeg program found at bin.
func New(dir, bin string, log *slog.Logger) *Optimizer {
	return &Optimizer{Dir: dir, Runner: execRunner{bin: bin}, Log: log, MinGain: 0.10}
}

// Available reports whether the ffmpeg program can be started (the job is skipped when it cannot).
func Available(bin string) bool {
	_, err := exec.LookPath(bin) // looks in PATH like a shell would
	return err == nil
}

// Report is the outcome of one run.
type Report struct {
	Seen, Shrunk, Kept, Failed int   // files looked at, shrunk, left as they were, that errored
	Before, After              int64 // bytes of the shrunk files before and after
}

// entry is one line of the manifest.
type entry struct {
	SHA     string `json:"sha"`     // fingerprint of the file as it is now
	Profile string `json:"profile"` // settings used
	Before  int64  `json:"before"`  // size before shrinking
	After   int64  `json:"after"`   // size now
	Gain    bool   `json:"gain"`    // false when ffmpeg could not make it smaller
}

// videoScale never enlarges a video and caps it at 960 x 540, so phones, tablets and TVs all keep their sharpness.
const videoScale = "scale='min(960,iw)':'min(540,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2"

// args returns the ffmpeg arguments that shrink one file, or nil when the kind is not handled.
// Sizes never grow (the "min" in each scale) and names never change, so links to the files keep working.
func args(in, out string) []string {
	switch strings.ToLower(filepath.Ext(in)) {
	case ".mp4": // H.264, web-ready
		return []string{"-i", in, "-map", "0:v:0", "-map", "0:a:0?", "-vf", videoScale,
			"-threads", "1", "-c:v", "libx264", "-preset", "slower", "-crf", "32", "-r", "24", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-c:a", "aac", "-b:a", "64k", "-f", "mp4", out}
	case ".webm": // VP9
		return []string{"-i", in, "-map", "0:v:0", "-map", "0:a:0?", "-vf", videoScale,
			"-threads", "1", "-c:v", "libvpx-vp9", "-crf", "38", "-b:v", "0", "-c:a", "libopus", "-b:a", "48k", "-f", "webm", out}
	case ".webp": // still photos: 1280 px wide at most (sharp on a TV)
		return []string{"-i", in, "-vf", "scale='min(1280,iw)':-2", "-c:v", "libwebp", "-quality", "65", "-compression_level", "6", "-frames:v", "1", "-f", "webp", out}
	case ".jpg", ".jpeg":
		return []string{"-i", in, "-vf", "scale='min(1280,iw)':-2", "-q:v", "6", "-frames:v", "1", "-f", "mjpeg", out}
	case ".m4a": // audio
		return []string{"-i", in, "-vn", "-c:a", "aac", "-b:a", "64k", "-f", "ipod", out}
	case ".mp3":
		return []string{"-i", in, "-vn", "-c:a", "libmp3lame", "-q:a", "6", "-f", "mp3", out}
	}
	return nil
}

// animated reports whether a WebP file holds an animation (ffmpeg would flatten it to one frame).
func animated(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()              // Spring: try-with-resources
	head := make([]byte, 64)     // the marker is in the first chunks
	n, _ := io.ReadFull(f, head) // a short file is fine
	return bytes.Contains(head[:n], []byte("ANIM")) || bytes.Contains(head[:n], []byte("ANMF"))
}

// sum returns the sha256 of a file as hex text.
func sum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Run shrinks every pending file once, one at a time, and updates the manifest after each file
// (so a stop in the middle loses nothing). A failure on one file never stops the others.
func (o *Optimizer) Run(ctx context.Context) (Report, error) {
	var rep Report
	manifest := map[string]entry{}
	mpath := filepath.Join(o.Dir, manifestName)
	if raw, err := os.ReadFile(mpath); err == nil {
		_ = json.Unmarshal(raw, &manifest) // a damaged manifest only means "start again"
	}
	save := func() error { // write the manifest through a temp file so it is never half written
		raw, _ := json.MarshalIndent(manifest, "", " ")
		tmp := mpath + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, mpath)
	}
	walkErr := filepath.WalkDir(o.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err() // stop on shutdown
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") || args(path, "") == nil {
			return nil // folders, hidden files and unknown kinds are not ours
		}
		rep.Seen++
		rel, _ := filepath.Rel(o.Dir, path)
		rel = filepath.ToSlash(rel)
		cur, err := sum(path)
		if err != nil {
			rep.Failed++
			o.Log.Warn("mediaopt: cannot read", "file", rel, "err", err)
			return nil
		}
		if e, ok := manifest[rel]; ok && e.SHA == cur && e.Profile == Profile {
			return nil // already done, nothing changed since
		}
		before, after, gain, err := o.one(ctx, path)
		switch {
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			return err
		case err != nil:
			rep.Failed++
			o.Log.Warn("mediaopt: failed", "file", rel, "err", err)
			return nil
		case gain:
			rep.Shrunk++
			rep.Before += before
			rep.After += after
			now, _ := sum(path) // the file was replaced: fingerprint the new one
			manifest[rel] = entry{SHA: now, Profile: Profile, Before: before, After: after, Gain: true}
		default:
			rep.Kept++
			manifest[rel] = entry{SHA: cur, Profile: Profile, Before: before, After: before, Gain: false}
		}
		return save()
	})
	if walkErr != nil {
		return rep, walkErr
	}
	return rep, save()
}

// one shrinks one file. It writes a temp file next to it and swaps it in only when it is at least MinGain smaller.
func (o *Optimizer) one(ctx context.Context, path string) (before, after int64, gain bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, false, err
	}
	before = info.Size()
	if strings.EqualFold(filepath.Ext(path), ".webp") && animated(path) {
		return before, before, false, nil // an animation stays as it is
	}
	tmp := filepath.Join(filepath.Dir(path), ".tmp-"+filepath.Base(path))
	defer os.Remove(tmp) // the temp file never stays; after a successful rename there is nothing left to remove
	if err := o.Runner.Run(ctx, args(path, tmp)...); err != nil {
		return before, before, false, err
	}
	out, err := os.Stat(tmp)
	if err != nil {
		return before, before, false, err
	}
	after = out.Size()
	if after == 0 || float64(after) > float64(before)*(1-o.MinGain) {
		return before, before, false, nil // not worth it: keep the original
	}
	if err := os.Rename(tmp, path); err != nil { // swap in the smaller file in one step
		return before, before, false, err
	}
	return before, after, true, nil
}
