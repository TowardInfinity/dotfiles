package memory

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Capture runs from a Stop hook that fires at the end of every assistant turn,
// asynchronously, with a ten-minute timeout. Several passes can therefore be
// alive at once — and Run is load-index, work, save-index with nothing in
// between, so two overlapping passes both find the same session due, both call
// Ollama on it, and whichever saves last silently discards the other's work.
//
// A lockfile makes the pass single-flight. Losing the race is the normal case,
// not an error: another process is already doing exactly this work, so the
// loser exits having done nothing and says so quietly.
const (
	lockName = "capture.lock"

	// lockStale bounds how long a crashed pass can block later ones. It sits
	// above the summarizer's per-call timeout so a genuinely slow Ollama run is
	// never mistaken for a dead process.
	lockStale = 15 * time.Minute
)

func lockPath() string { return filepath.Join(filepath.Dir(IndexPath()), lockName) }

// acquireLock takes the capture lock. It returns ok=false when another pass
// holds it, in which case release is nil and the caller must not do any work.
func acquireLock() (release func(), ok bool) {
	path := lockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false
	}

	if f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
		_ = f.Close()
		return func() { _ = os.Remove(path) }, true
	}

	// Held. If it is old enough that no live pass could still own it, the
	// holder died without cleaning up — clear it and try once more. A single
	// retry is deliberate: two processes racing to break the same stale lock
	// both fail the O_EXCL below, and both correctly decline to run.
	st, err := os.Stat(path)
	if err != nil || time.Since(st.ModTime()) < lockStale {
		return nil, false
	}
	if err := os.Remove(path); err != nil {
		return nil, false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, false
	}
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
	_ = f.Close()
	return func() { _ = os.Remove(path) }, true
}
