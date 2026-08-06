package logtail

import (
	"context"
	"io"
	"os"
	"sync"
	"time"
)

// FileTailer tails a single file (e.g. Player.log) from a tracked offset.
// It starts at the current end of the file on first poll, so only new lines
// are emitted.
type FileTailer struct {
	Path   string
	Lines  chan string

	pollInterval time.Duration

	mu      sync.Mutex
	stopped bool
	done    chan struct{}
	offset  int64
}

// NewFileTailer creates a tailer for a single file path. Start must be called
// to begin polling. Path can be empty (Start will return nil safely).
func NewFileTailer(path string) *FileTailer {
	return &FileTailer{
		Path:         path,
		Lines:        make(chan string, 256),
		pollInterval: 500 * time.Millisecond,
		done:         make(chan struct{}),
	}
}

// Start begins polling the file. Returns nil immediately; errors are silently
// handled (file not ready yet, etc).
func (ft *FileTailer) Start(ctx context.Context) error {
	go ft.loop(ctx)
	return nil
}

// SetPath updates the file path live and resets offset.
func (ft *FileTailer) SetPath(path string) {
	ft.mu.Lock()
	ft.Path = path
	ft.offset = 0
	ft.mu.Unlock()
}

func (ft *FileTailer) loop(ctx context.Context) {
	defer close(ft.Lines)
	ticker := time.NewTicker(ft.pollInterval)
	defer ticker.Stop()
	for {
		ft.pollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ft.done:
			return
		case <-ticker.C:
		}
	}
}

func (ft *FileTailer) pollOnce(ctx context.Context) {
	ft.mu.Lock()
	path := ft.Path
	offset := ft.offset
	ft.mu.Unlock()
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	sz := info.Size()
	if offset == 0 {
		// First poll: start at end.
		offset = sz
	}
	if sz < offset {
		// truncated / rotated: restart from the end on the next poll
		ft.mu.Lock()
		if ft.Path == path {
			ft.offset = 0
		}
		ft.mu.Unlock()
		return
	}
	if sz <= offset {
		// Persist the offset (including the start-at-end offset from the
		// first poll) so the next poll reads from a non-zero position.
		// Without this write-back, offset stays 0 and every poll restarts
		// at the end, so the tailer would never emit anything.
		ft.mu.Lock()
		if ft.Path == path {
			ft.offset = offset
		}
		ft.mu.Unlock()
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return
	}
	buf := make([]byte, sz-offset)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return
	}
	text := buf[:n]
	newOffset := offset + int64(n)
	lastNL := -1
	for i, c := range text {
		if c == '\n' {
			lastNL = i
		}
	}
	if lastNL >= 0 && lastNL < len(text)-1 {
		newOffset -= int64(len(text) - 1 - lastNL)
		text = text[:lastNL+1]
	} else if lastNL < 0 && len(text) > 0 {
		newOffset -= int64(len(text))
		ft.mu.Lock()
		if ft.Path == path {
			ft.offset = newOffset
		}
		ft.mu.Unlock()
		return
	}
	// Write the new offset back only if the path is unchanged since we read it —
	// a concurrent SetPath resets both, and clobbering that would seek the new
	// file at a stale offset.
	ft.mu.Lock()
	if ft.Path == path {
		ft.offset = newOffset
	}
	ft.mu.Unlock()
	for _, line := range splitLines(text) {
		if !ft.send(ctx, line) {
			return
		}
	}
}

func (ft *FileTailer) send(ctx context.Context, line string) bool {
	select {
	case ft.Lines <- line:
		return true
	case <-ctx.Done():
		return false
	case <-ft.done:
		return false
	}
}

// Close stops the poller.
func (ft *FileTailer) Close() {
	ft.mu.Lock()
	if ft.stopped {
		ft.mu.Unlock()
		return
	}
	ft.stopped = true
	ft.mu.Unlock()
	select {
	case <-ft.done:
	default:
		close(ft.done)
	}
}
