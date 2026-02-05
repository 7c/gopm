package logwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
