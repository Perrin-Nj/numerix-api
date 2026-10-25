package mediaopt

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// fakeRunner stands in for ffmpeg: it writes an output file of a chosen size (Spring: a hand written mock).
type fakeRunner struct {
	ratio float64 // output size = input size x ratio
	calls int     // how many files it was asked to process
}

// Run copies "ratio" of the input into the output file (the last argument).
func (f *fakeRunner) Run(_ context.Context, args ...string) error {
	f.calls++
	in, out := args[1], args[len(args)-1] // "-i", input ... output
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	return os.WriteFile(out, raw[:int(float64(len(raw))*f.ratio)], 0o644)
}

// write puts n bytes into a file of the test folder.
func write(t *testing.T, dir, name string, n int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// newOpt builds an Optimizer on a fake ffmpeg.
func newOpt(dir string, ratio float64) (*Optimizer, *fakeRunner) {
	r := &fakeRunner{ratio: ratio}
	return &Optimizer{Dir: dir, Runner: r, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), MinGain: 0.10}, r
}

// TestShrinksOnceAndNeverAgain: a file that shrinks by half is replaced, then left alone on the next run.
func TestShrinksOnceAndNeverAgain(t *testing.T) {
	dir := t.TempDir()
	vid := write(t, dir, "clip.mp4", 10000)
	opt, fake := newOpt(dir, 0.4)
	rep, err := opt.Run(context.Background())
	if err != nil || rep.Shrunk != 1 || rep.Before != 10000 || rep.After != 4000 {
		t.Fatalf("first run = %+v, %v", rep, err)
	}
	if st, _ := os.Stat(vid); st.Size() != 4000 {
		t.Fatalf("file size = %d, want 4000", st.Size())
	}
	rep, err = opt.Run(context.Background())
	if err != nil || fake.calls != 1 || rep.Shrunk != 0 {
		t.Fatalf("second run must do nothing: %+v calls=%d %v", rep, fake.calls, err)
	}
}

// TestKeepsTheOriginalWhenTheGainIsSmall: a result less than 10 percent smaller is thrown away.
func TestKeepsTheOriginalWhenTheGainIsSmall(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "photo.webp", 10000)
	opt, _ := newOpt(dir, 0.95)
	rep, err := opt.Run(context.Background())
	if err != nil || rep.Kept != 1 || rep.Shrunk != 0 {
		t.Fatalf("run = %+v, %v", rep, err)
	}
	if st, _ := os.Stat(p); st.Size() != 10000 {
		t.Fatalf("original changed: %d", st.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, ".tmp-photo.webp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

// TestNewFilesAreCaughtAndOtherKindsIgnored: a file added later is processed; captions and hidden files are not touched.
func TestNewFilesAreCaughtAndOtherKindsIgnored(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.mp4", 10000)
	write(t, dir, "captions.vtt", 500)
	opt, fake := newOpt(dir, 0.3)
	if _, err := opt.Run(context.Background()); err != nil || fake.calls != 1 {
		t.Fatalf("calls = %d, %v", fake.calls, err)
	}
	write(t, dir, "b.m4a", 10000) // dropped in after the first run
	rep, _ := opt.Run(context.Background())
	if rep.Shrunk != 1 || fake.calls != 2 {
		t.Fatalf("new file not processed: %+v calls=%d", rep, fake.calls)
	}
}

// TestAnimatedWebpIsLeftAlone: ffmpeg would flatten an animation, so it is skipped.
func TestAnimatedWebpIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "move.webp")
	if err := os.WriteFile(p, append([]byte("RIFFxxxxWEBPVP8X....ANIM"), make([]byte, 5000)...), 0o644); err != nil {
		t.Fatal(err)
	}
	opt, fake := newOpt(dir, 0.1)
	rep, _ := opt.Run(context.Background())
	if fake.calls != 0 || rep.Shrunk != 0 {
		t.Fatalf("animated file was processed: %+v calls=%d", rep, fake.calls)
	}
}
