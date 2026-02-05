package logwriter

import (
	"fmt"
	"os"
	"sync"
)

// RotatingWriter implements io.Writer with size-based log rotation.
type RotatingWriter struct {
	path     string
	maxSize  int64
	maxFiles int
	current  *os.File
	written  int64
	mu       sync.Mutex
}

// New creates a new RotatingWriter. maxSize is in bytes, maxFiles is the number
// of rotated files to keep (e.g. 3 means .1, .2, .3).
func New(path string, maxSize int64, maxFiles int) (*RotatingWriter, error) {
	if maxSize <= 0 {
		maxSize = 1048576 // 1MB default
	}
	if maxFiles <= 0 {
		maxFiles = 3
	}

	w := &RotatingWriter{
		path:     path,
		maxSize:  maxSize,
		maxFiles: maxFiles,
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	w.current = f
	w.written = info.Size()
	return w, nil
}

// Write implements io.Writer. It rotates the file if maxSize is exceeded.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.current == nil {
		return 0, fmt.Errorf("writer is closed")
	}

	if w.written+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.current.Write(p)
	w.written += int64(n)
	return n, err
}

// rotate shifts log files: .2→delete, .1→.2, current→.1, open fresh.
func (w *RotatingWriter) rotate() error {
	w.current.Close()

	// Shift rotated files
	for i := w.maxFiles; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		if i == w.maxFiles {
			os.Remove(src)
		} else {
			dst := fmt.Sprintf("%s.%d", w.path, i+1)
			os.Rename(src, dst)
		}
	}

	// Move current to .1
	os.Rename(w.path, fmt.Sprintf("%s.1", w.path))

	// Open fresh file
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	w.current = f
	w.written = 0
	return nil
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		err := w.current.Close()
		w.current = nil
		return err
	}
	return nil
}

// Truncate clears the current log file.
func (w *RotatingWriter) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil {
		return nil
	}
	w.current.Close()
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	w.current = f
	w.written = 0
	return nil
}

// Path returns the file path of this writer.
func (w *RotatingWriter) Path() string {
	return w.path
}
