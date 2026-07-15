package logwriter

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := New(path, 100, 2) // 100 bytes max, keep 2 rotated files
	if err != nil {
		t.Fatal(err)
	}

	// Write some data
	msg := "hello world\n"
	n, err := w.Write([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if n != len(msg) {
		t.Errorf("Write returned %d, want %d", n, len(msg))
	}

	if w.Path() != path {
		t.Errorf("Path() = %q, want %q", w.Path(), path)
	}

	w.Close()

	// Verify content
	data, _ := os.ReadFile(path)
	if string(data) != msg {
		t.Errorf("file content = %q, want %q", data, msg)
	}
}

func TestRotatingWriterRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := New(path, 50, 2) // 50 bytes triggers rotation
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write enough to trigger rotation
	line := strings.Repeat("X", 30) + "\n" // 31 bytes per line
	w.Write([]byte(line))                   // 31 bytes
	w.Write([]byte(line))                   // 62 bytes -> triggers rotation
	w.Write([]byte(line))                   // new file, 31 bytes

	// Check that rotated file exists
	if _, err := os.Stat(path + ".1"); os.IsNotExist(err) {
		t.Error("rotated file .1 should exist")
	}

	// Current file should have last write
	data, _ := os.ReadFile(path)
	if len(data) != 31 {
		t.Errorf("current file size = %d, want 31", len(data))
	}
}

// TestRotatingWriterOnRotateMayWriteBack guards a self-deadlock: the daemon's
// OnRotate callback logs through the same writer that just rotated. When the
// callback ran with the writer's lock held, that write blocked forever on a
// non-reentrant mutex and wedged every goroutine that logged afterwards.
func TestRotatingWriterOnRotateMayWriteBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := New(path, 50, 2)
	if err != nil {
		t.Fatal(err)
	}

	gotRotations := make(chan int, 1)
	w.OnRotate = func(_ string, rotations int) {
		gotRotations <- rotations
		w.Write([]byte("rotated\n")) // re-enter the writer
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		line := strings.Repeat("X", 30) + "\n" // 31 bytes
		w.Write([]byte(line))                  // 31 bytes
		w.Write([]byte(line))                  // 62 bytes -> rotates
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Do not Close here: the wedged goroutine still holds the lock.
		t.Fatal("Write deadlocked; OnRotate must run after the lock is released")
	}

	if r := <-gotRotations; r != 1 {
		t.Errorf("OnRotate got rotations = %d, want 1", r)
	}

	w.Close()

	data, _ := os.ReadFile(path)
	want := strings.Repeat("X", 30) + "\nrotated\n"
	if string(data) != want {
		t.Errorf("current file = %q, want %q", data, want)
	}
}

// TestRotatingWriterOnRotateNoRecursion covers a callback whose own notice is
// larger than maxSize, so writing it rotates again. That nested rotation must
// not re-invoke OnRotate.
func TestRotatingWriterOnRotateNoRecursion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := New(path, 20, 2)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w.OnRotate = func(_ string, _ int) {
		calls.Add(1)
		w.Write([]byte(strings.Repeat("N", 40) + "\n")) // 41 bytes > maxSize
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Write([]byte(strings.Repeat("X", 15) + "\n")) // 16 bytes
		w.Write([]byte(strings.Repeat("X", 15) + "\n")) // 32 bytes -> rotates
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Write did not return; OnRotate recursed or deadlocked")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("OnRotate called %d times, want 1", got)
	}
	w.Close()
}

// TestRotatingWriterOnRotateSyncSlogDeadlocks documents the second layer of
// the deadlock that b664038 alone did not fix. slog.Handler.Handle holds its
// own mutex across w.Write, so if OnRotate logs synchronously through the
// same handler, the nested Handle blocks on that mutex — even though
// RotatingWriter has already released its own lock. The whole daemon then
// wedges. The daemon must schedule the notice on a fresh goroutine.
func TestRotatingWriterOnRotateSyncSlogDeadlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := New(path, 200, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	logger := slog.New(slog.NewTextHandler(w, nil))

	w.OnRotate = func(p string, rotations int) {
		// The dangerous pattern: re-enters TextHandler.Handle while its
		// outer invocation still holds h.mu. Must not be used in the
		// daemon; kept here to lock in the guarantee that this pattern
		// deadlocks so a future refactor cannot silently ship it.
		logger.Info("daemon log rotated", "path", p, "rotations", rotations)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			logger.Info("filler", "i", i, "pad", strings.Repeat("x", 40))
		}
	}()

	select {
	case <-done:
		t.Fatal("sync slog inside OnRotate should have deadlocked on TextHandler.mu, but did not — writer semantics changed and daemon.go's async goroutine fix may no longer be needed (or another bug is masking this)")
	case <-time.After(2 * time.Second):
		// Expected: deadlock.
	}
}

// TestRotatingWriterOnRotateAsyncSlogSucceeds pins the daemon's correct
// pattern: deferring the rotation notice to a fresh goroutine escapes the
// outer TextHandler.Handle stack frame and its mutex, so logging completes.
func TestRotatingWriterOnRotateAsyncSlogSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Size chosen so exactly one rotation fires from the writes below and
	// the follow-up notice stays in the fresh file. Multiple rotations
	// would race the async notice into rotated files and make this test
	// non-deterministic.
	w, err := New(path, 2000, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	logger := slog.New(slog.NewTextHandler(w, nil))

	notice := make(chan struct{}, 1)
	w.OnRotate = func(p string, rotations int) {
		go func() {
			logger.Info("daemon log rotated", "path", p, "rotations", rotations)
			notice <- struct{}{}
		}()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// ~120 bytes per line, ~2000 bytes total triggers one rotation
		// and leaves plenty of room for the notice.
		for i := 0; i < 15; i++ {
			logger.Info("filler", "i", i, "pad", strings.Repeat("x", 80))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logging through slog deadlocked on the TextHandler mutex")
	}

	select {
	case <-notice:
	case <-time.After(5 * time.Second):
		t.Fatal("OnRotate never wrote the rotation notice through slog")
	}

	if _, err := os.Stat(path + ".1"); os.IsNotExist(err) {
		t.Error("rotated file .1 should exist")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "daemon log rotated") {
		t.Errorf("current file missing rotation notice; got %q", data)
	}
}

func TestRotatingWriterTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := New(path, 1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write([]byte("some data\n"))

	if err := w.Truncate(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if len(data) != 0 {
		t.Errorf("file should be empty after truncate, got %d bytes", len(data))
	}
}

func TestTimestampWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rot, err := New(path, 1024*1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	tw := NewTimestampWriter(rot)

	// Write a complete line
	tw.Write([]byte("hello world\n"))

	// Write a partial line, then finish it
	tw.Write([]byte("partial"))
	tw.Write([]byte(" line\n"))

	tw.Underlying().Close()

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(data))
	}

	// Each line should start with a timestamp (ISO-8601 format)
	for i, line := range lines {
		// Timestamp format: 2006-01-02T15:04:05.000Z07:00
		if len(line) < 30 || line[4] != '-' || line[10] != 'T' {
			t.Errorf("line %d missing timestamp prefix: %q", i, line)
		}
	}

	// Check that content is preserved after timestamp
	if !strings.HasSuffix(lines[0], " hello world") {
		t.Errorf("line 0 content wrong: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], " partial line") {
		t.Errorf("line 1 content wrong: %q", lines[1])
	}
}
