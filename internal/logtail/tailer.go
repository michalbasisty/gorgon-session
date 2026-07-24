// Package logtail follows Project Gorgon's chat-log directory exactly the
// way GorgonSurveyTracker does: poll the configured ChatLogs folder every
// pollInterval, pick the newest *.log by mtime, and tail it from a remembered
// byte offset. When the game rolls to a new day's log file (a different file
// becomes the newest), the tailer switches to it and resets its offset.
//
// This mirrors survey_tracker.py's _poll_chat_log() (a 500ms QTimer calling
// glob("*.log") + st_mtime sort + seek/read). It is intentionally
// cross-platform and does not depend on fsnotify, so it works on Windows,
// macOS and Linux identically.
package logtail

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Tailer polls a folder for the most recent *.log file and streams appended
// lines on Lines.
type Tailer struct {
	Dir   string
	Lines chan string

	pollInterval time.Duration

	mu      sync.Mutex
	stopped bool
	done    chan struct{}

	curFile   string
	curOffset int64
}

// New constructs a Tailer for the given chat-logs directory. Start must be
// called to begin polling.
func New(dir string) *Tailer {
	var cleaned string
	if dir != "" {
		cleaned = filepath.Clean(dir)
	}
	return &Tailer{
		Dir:          cleaned,
		Lines:        make(chan string, 256),
		pollInterval: 500 * time.Millisecond,
		done:         make(chan struct{}),
	}
}

// Start spawns the polling goroutine and returns immediately. The directory
// does not need to exist yet; if it is missing or empty, the poller just
// waits and starts emitting lines once the game creates the first *.log.
func (t *Tailer) Start(ctx context.Context) error {
	go t.loop(ctx)
	return nil
}

func (t *Tailer) loop(ctx context.Context) {
	defer close(t.Lines)
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()
	for {
		t.pollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			return
		case <-ticker.C:
		}
	}
}

// SetDir updates the directory being tailed live.
func (t *Tailer) SetDir(dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if dir == "" {
		t.Dir = ""
	} else {
		t.Dir = filepath.Clean(dir)
	}
	t.curFile = ""
	t.curOffset = 0
}

// pollOnce finds the newest *.log file in t.Dir, reads any bytes past the
// remembered offset, and emits complete lines (newline-stripped) on Lines.
func (t *Tailer) pollOnce(ctx context.Context) {
	t.mu.Lock()
	dir := t.Dir
	t.mu.Unlock()

	if dir == "" || dir == "." {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var newest string
	var newestMT int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".log" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime().UnixNano()
		if mt > newestMT {
			newestMT = mt
			newest = filepath.Join(dir, e.Name())
		}
	}
	if newest == "" {
		return
	}
	info, err := os.Stat(newest)
	if err != nil {
		return
	}
	sz := info.Size()
	if newest != t.curFile {
		// New day / new file: start at END to only capture new lines.
		t.curFile = newest
		t.curOffset = sz
	}
	if sz < t.curOffset {
		// truncated (game restart); skip to end.
		t.curOffset = sz
	}
	if sz <= t.curOffset {
		return
	}
	f, err := os.Open(newest)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(t.curOffset, io.SeekStart); err != nil {
		return
	}
	buf := make([]byte, sz-t.curOffset)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return
	}
	// Emit complete lines; any trailing partial is kept for next poll by
	// rewinding offset back to the last newline.
	text := buf[:n]
	t.curOffset += int64(n)
	lastNL := -1
	for i, c := range text {
		if c == '\n' {
			lastNL = i
		}
	}
	if lastNL >= 0 && lastNL < len(text)-1 {
		// trailing partial line: rewind offset to start of partial so we
		// re-read the remainder next tick when more bytes arrive.
		t.curOffset -= int64(len(text) - 1 - lastNL)
		text = text[:lastNL+1]
	} else if lastNL < 0 && len(text) > 0 {
		// Whole read had no newline at all; rewind all of it for next poll.
		t.curOffset -= int64(len(text))
		return
	}
	for _, line := range splitLines(text) {
		if !t.send(ctx, line) {
			return
		}
	}
}

func (t *Tailer) send(ctx context.Context, line string) bool {
	select {
	case t.Lines <- line:
		return true
	case <-ctx.Done():
		return false
	case <-t.done:
		return false
	}
}

// Close stops the poller and closes its Lines channel.
func (t *Tailer) Close() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	t.mu.Unlock()
	select {
	case <-t.done:
	default:
		close(t.done)
	}
}

// splitLines splits newline-delimited bytes, stripping trailing CR.
func splitLines(b []byte) []string {
	out := []string{}
	start := 0
	for i, c := range b {
		if c == '\n' {
			end := i
			if end > start && b[end-1] == '\r' {
				end--
			}
			out = append(out, string(b[start:end]))
			start = i + 1
		}
	}
	if start < len(b) {
		end := len(b)
		if end > start && b[end-1] == '\r' {
			end--
		}
		out = append(out, string(b[start:end]))
	}
	return out
}

// String is a debug helper.
func (t *Tailer) String() string {
	t.mu.Lock()
	dir := t.Dir
	t.mu.Unlock()
	return fmt.Sprintf("logtail.Tailer(dir=%s, file=%s, off=%d)", dir, t.curFile, t.curOffset)
}