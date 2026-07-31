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
	"io"
	"log"
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
	log.Printf("tailer: starting, watching directory: %q", t.Dir)
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
	curFile, curOffset := t.curFile, t.curOffset
	t.mu.Unlock()

	if dir == "" || dir == "." {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("tailer: ReadDir %q failed: %v", dir, err)
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
		log.Printf("tailer: no .log files found in %q", dir)
		return
	}
	if newest != curFile {
		log.Printf("tailer: switching to newest log: %q (was %q)", newest, curFile)
	}
	info, err := os.Stat(newest)
	if err != nil {
		log.Printf("tailer: Stat %q failed: %v", newest, err)
		return
	}
	sz := info.Size()
	if newest != curFile {
		// New day / new file: start at END to only capture new lines.
		curFile = newest
		curOffset = sz
	}
	if sz < curOffset {
		// truncated (game restart); skip to end.
		curOffset = sz
	}
	if sz <= curOffset {
		// Nothing new to read, but persist the current file/offset first:
		// otherwise a fresh file would re-"switch" (and re-reset to EOF)
		// on every poll and never read any lines.
		t.mu.Lock()
		t.curFile = curFile
		t.curOffset = curOffset
		t.mu.Unlock()
		return
	}
	f, err := os.Open(newest)
	if err != nil {
		log.Printf("tailer: Open %q failed: %v", newest, err)
		return
	}
	defer f.Close()
	if _, err := f.Seek(curOffset, io.SeekStart); err != nil {
		log.Printf("tailer: Seek %q to %d failed: %v", newest, curOffset, err)
		return
	}
	buf := make([]byte, sz-curOffset)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		log.Printf("tailer: ReadFull %q failed: %v", newest, err)
		return
	}
	// Emit complete lines; any trailing partial is kept for next poll by
	// rewinding offset back to the last newline.
	text := buf[:n]
	curOffset += int64(n)
	lastNL := -1
	for i, c := range text {
		if c == '\n' {
			lastNL = i
		}
	}
	if lastNL >= 0 && lastNL < len(text)-1 {
		// trailing partial line: rewind offset to start of partial so we
		// re-read the remainder next tick when more bytes arrive.
		curOffset -= int64(len(text) - 1 - lastNL)
		text = text[:lastNL+1]
	} else if lastNL < 0 && len(text) > 0 {
		// Whole read had no newline at all; keep offset to re-read next poll.
		return
	}

	t.mu.Lock()
	t.curFile = curFile
	t.curOffset = curOffset
	t.mu.Unlock()

	lines := splitLines(text)
	if len(lines) > 0 {
		log.Printf("tailer: read %d lines from %q", len(lines), newest)
	}
	for _, line := range lines {
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

