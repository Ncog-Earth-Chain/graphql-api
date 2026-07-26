package solidity

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCompilerCacheSurvivesConcurrentResolution pins the synchronisation on
// CompilerManager.compilers.
//
// One repository instance is shared process-wide (repository.R(), a sync.Once singleton) and
// graphql-go resolves a request's root fields concurrently: AvailableCompilerVersions returns
// an error, which marks the field Async, so a single unauthenticated HTTP POST aliasing that
// field N times fans out across up to MaxParallelism (default 10) goroutines, all walking
// every entry of solidityReleases and writing this map. Unsynchronised, the Go runtime
// detects the collision and calls fatal() -- which no panic handler can recover, so the whole
// API process dies. This reproduced as "fatal error: concurrent map writes" at the
// cm.compilers write before the lock was added.
//
// Every release is planted locally so resolution always takes the exec.LookPath success
// branch -- the branch that writes the map -- and never reaches the network.
func TestCompilerCacheSurvivesConcurrentResolution(t *testing.T) {
	base := t.TempDir()
	for _, v := range solidityReleases {
		n := strings.TrimPrefix(v, "v")
		for _, name := range []string{"solc-" + n, "solc-" + n + ".exe"} {
			if err := os.WriteFile(filepath.Join(base, name), []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	cm := NewCompilerManager(base, filepath.Join(base, "solc-none"))

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = cm.GetAvailableVersions()
			}
		}()
	}
	wg.Wait()

	if got := len(cm.GetAvailableVersions()); got != len(solidityReleases) {
		t.Errorf("resolved %d versions, want %d", got, len(solidityReleases))
	}
}

// TestAvailableVersionsDoesNotReachTheNetwork guards a DoS amplification.
//
// IsVersionSupported used to call GetCompilerPath, which falls through to
// downloadAndInstallCompiler. Because GetAvailableVersions loops over every entry of
// solidityReleases, the unauthenticated GraphQL field `availableCompilerVersions` made the
// server attempt one solc download per release from github.com -- on the order of a gigabyte
// -- each with a 5-minute client timeout, on any instance whose compiler directory was cold.
// Asking what is available must not be an instruction to fetch it.
//
// An EMPTY compiler directory is the worst case: nothing resolves locally, so the old code
// fell through to the download path for every release.
func TestAvailableVersionsDoesNotReachTheNetwork(t *testing.T) {
	var calls int64
	orig := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt64(&calls, 1)
		return nil, http.ErrUseLastResponse
	})
	t.Cleanup(func() { http.DefaultTransport = orig })

	cm := NewCompilerManager(t.TempDir(), "solc-none")

	done := make(chan []string, 1)
	go func() { done <- cm.GetAvailableVersions() }()

	select {
	case got := <-done:
		if n := atomic.LoadInt64(&calls); n != 0 {
			t.Errorf("GetAvailableVersions made %d outbound HTTP calls; it must resolve locally only", n)
		}
		if len(got) != 0 {
			t.Errorf("with an empty compiler directory it reported %d available versions, want 0", len(got))
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("GetAvailableVersions did not return within 20s; it is still reaching the network (%d calls so far)",
			atomic.LoadInt64(&calls))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
