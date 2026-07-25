package logger

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLogOutput pins the log-sink resolution: the standard streams by name, a filesystem path
// opened for append, and a graceful fallback to stderr for an unopenable path (a bad log
// destination must never stop the server from starting).
func TestLogOutput(t *testing.T) {
	t.Run("stderr and empty default to os.Stderr", func(t *testing.T) {
		for _, in := range []string{"", "stderr", "  STDERR  "} {
			if got := logOutput(in); got != os.Stderr {
				t.Errorf("logOutput(%q) = %v, want os.Stderr", in, got)
			}
		}
	})

	t.Run("stdout maps to os.Stdout", func(t *testing.T) {
		if got := logOutput("stdout"); got != os.Stdout {
			t.Errorf("logOutput(\"stdout\") = %v, want os.Stdout", got)
		}
	})

	t.Run("a path opens a file that receives writes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "explorer.log")
		w := logOutput(path)
		f, ok := w.(*os.File)
		if !ok {
			t.Fatalf("logOutput(path) = %T, want *os.File", w)
		}
		t.Cleanup(func() { _ = f.Close() })

		if _, err := f.WriteString("hello\n"); err != nil {
			t.Fatalf("write to log file: %v", err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back log file: %v", err)
		}
		if string(b) != "hello\n" {
			t.Errorf("log file contents = %q, want %q", b, "hello\n")
		}
	})

	t.Run("an unopenable path falls back to stderr", func(t *testing.T) {
		// A file inside a directory that does not exist cannot be created.
		bad := filepath.Join(t.TempDir(), "no-such-dir", "explorer.log")
		if got := logOutput(bad); got != os.Stderr {
			t.Errorf("logOutput(unopenable) = %v, want os.Stderr fallback", got)
		}
	})
}
