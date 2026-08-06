package logtail

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{}},
		{"lf only", "a\nb\n", []string{"a", "b"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
		{"mixed endings", "a\r\nb\nc\r\n", []string{"a", "b", "c"}},
		{"no trailing newline", "a\nb", []string{"a", "b"}},
		{"single line no newline", "hello", []string{"hello"}},
		{"lone crlf no trailing", "a\r\nb", []string{"a", "b"}},
		{"blank line", "a\n\nb\n", []string{"a", "", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := splitLines([]byte(c.in)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("splitLines(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// expectLines reads exactly n lines from ch within a generous timeout.
func expectLines(t *testing.T, ch <-chan string, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	timeout := time.After(3 * time.Second)
	for len(out) < n {
		select {
		case line, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d lines, want %d (got %v)", len(out), n, out)
			}
			out = append(out, line)
		case <-timeout:
			t.Fatalf("timeout waiting for %d lines, got %d: %v", n, len(out), out)
		}
	}
	return out
}

// expectNothing asserts no line arrives within d.
func expectNothing(t *testing.T, ch <-chan string, d time.Duration) {
	t.Helper()
	select {
	case line, ok := <-ch:
		if ok {
			t.Fatalf("unexpected line: %q", line)
		}
		t.Fatal("channel closed unexpectedly")
	case <-time.After(d):
	}
}

func waitForClose(t *testing.T, ch <-chan string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("channel was not closed")
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendToFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append to %s: %v", path, err)
	}
}

func TestTailerTailsNewestLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ChatLog.log")
	writeFile(t, logPath, "line1\nline2\n")

	tr := New(dir)
	tr.pollInterval = 20 * time.Millisecond // fast polls; Start must be called first

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First poll switches to the pre-existing file and starts at its END, so
	// line1/line2 must not be emitted. Give it a few polls to settle.
	time.Sleep(300 * time.Millisecond)
	expectNothing(t, tr.Lines, 300*time.Millisecond)

	// Appended lines arrive.
	appendToFile(t, logPath, "line3\nline4\n")
	if got := expectLines(t, tr.Lines, 2); !reflect.DeepEqual(got, []string{"line3", "line4"}) {
		t.Fatalf("got %q, want [line3 line4]", got)
	}

	// A .txt file (even with a newer mtime) is ignored.
	notesPath := filepath.Join(dir, "notes.txt")
	writeFile(t, notesPath, "junk\n")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(notesPath, future, future); err != nil {
		t.Fatalf("Chtimes notes.txt: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	expectNothing(t, tr.Lines, 300*time.Millisecond)
	appendToFile(t, logPath, "line5\n")
	if got := expectLines(t, tr.Lines, 1); got[0] != "line5" {
		t.Fatalf("got %q, want [line5] (still tailing ChatLog.log)", got)
	}

	// A newer .log file: the tailer switches to it, starting at its end, and
	// emits lines appended after the switch.
	newPath := filepath.Join(dir, "NewDay.log")
	writeFile(t, newPath, "day2 pre-existing\n")
	future2 := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(newPath, future2, future2); err != nil {
		t.Fatalf("Chtimes NewDay.log: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // let the switch poll run (offset = end)
	expectNothing(t, tr.Lines, 300*time.Millisecond)
	appendToFile(t, newPath, "day2 new\n")
	if got := expectLines(t, tr.Lines, 1); got[0] != "day2 new" {
		t.Fatalf("got %q, want [day2 new]", got)
	}

	cancel()
	waitForClose(t, tr.Lines)
}

func TestFileTailerStartsAtEndAndRewindsPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "player.log")
	writeFile(t, path, "old line\n")

	ft := NewFileTailer(path)
	ft.pollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ft.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First poll starts at the END: pre-existing content is not emitted.
	time.Sleep(300 * time.Millisecond)
	expectNothing(t, ft.Lines, 300*time.Millisecond)

	// Partial line without newline: nothing emitted yet.
	appendToFile(t, path, "partial")
	time.Sleep(300 * time.Millisecond)
	expectNothing(t, ft.Lines, 300*time.Millisecond)

	// Completion of the line arrives as a single line (partial-line rewind).
	appendToFile(t, path, "complete\n")
	if got := expectLines(t, ft.Lines, 1); got[0] != "partialcomplete" {
		t.Fatalf("got %q, want [partialcomplete]", got)
	}

	// SetPath: new file, offset reset — pre-existing content of the new file
	// is skipped (no stale-offset garbage), appended lines arrive.
	path2 := filepath.Join(dir, "player2.log")
	writeFile(t, path2, "old2\n")
	ft.SetPath(path2)
	time.Sleep(300 * time.Millisecond)
	expectNothing(t, ft.Lines, 300*time.Millisecond)
	appendToFile(t, path2, "fresh2\n")
	if got := expectLines(t, ft.Lines, 1); got[0] != "fresh2" {
		t.Fatalf("got %q, want [fresh2]", got)
	}
	expectNothing(t, ft.Lines, 200*time.Millisecond)

	cancel()
	waitForClose(t, ft.Lines)
}
