package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Config parsing tests
// ---------------------------------------------------------------------------

func resetFlags() {
	// Reset the flag package so each test starts clean.
	// parseConfig registers flags on flag.CommandLine, which is global state.
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

func TestParseConfig_FromFlags(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"static-server", "--networks=mainnet,tethys", "--poll-interval=30s", "--cache-dir=/tmp/test"}

	cfg := parseConfig()

	if len(cfg.Networks) != 2 || cfg.Networks[0] != "mainnet" || cfg.Networks[1] != "tethys" {
		t.Fatalf("expected Networks=[mainnet tethys], got %v", cfg.Networks)
	}
	if cfg.PollInterval != 30*time.Second {
		t.Fatalf("expected PollInterval=30s, got %v", cfg.PollInterval)
	}
	if cfg.CacheDir != "/tmp/test" {
		t.Fatalf("expected CacheDir=/tmp/test, got %v", cfg.CacheDir)
	}
}

func TestParseConfig_DefaultPollInterval(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"static-server", "--networks=mainnet"}

	cfg := parseConfig()

	if cfg.PollInterval != 60*time.Second {
		t.Fatalf("expected default PollInterval=60s, got %v", cfg.PollInterval)
	}
}

func TestParseConfig_DefaultCacheDir(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"static-server", "--networks=mainnet"}

	cfg := parseConfig()

	if cfg.CacheDir != "/tmp/cache" {
		t.Fatalf("expected default CacheDir=/tmp/cache, got %v", cfg.CacheDir)
	}
}

func TestParseConfig_FromEnv(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	t.Setenv("NETWORKS", "mainnet,tethys")
	os.Args = []string{"static-server"}

	cfg := parseConfig()

	if len(cfg.Networks) != 2 || cfg.Networks[0] != "mainnet" || cfg.Networks[1] != "tethys" {
		t.Fatalf("expected Networks=[mainnet tethys], got %v", cfg.Networks)
	}
}

func TestParseConfig_NoNetworks(t *testing.T) {
	// Use the subprocess test pattern: re-invoke the test binary with a
	// special env var to detect os.Exit(1).
	if os.Getenv("TEST_NO_NETWORKS_SUBPROCESS") == "1" {
		resetFlags()
		os.Args = []string{"static-server"}
		parseConfig()
		// If we get here, parseConfig didn't exit -- fail the subprocess.
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestParseConfig_NoNetworks$")
	cmd.Env = append(os.Environ(), "TEST_NO_NETWORKS_SUBPROCESS=1")
	err := cmd.Run()

	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return // Expected: parseConfig called os.Exit(1)
		}
	}
	if err == nil {
		t.Fatal("expected parseConfig to call os.Exit(1) when no networks configured, but it exited 0")
	}
	t.Fatalf("expected exit code 1, got error: %v", err)
}

func TestParseConfig_ListenAddr(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"static-server", "--networks=mainnet", "--listen-addr=:9090"}

	cfg := parseConfig()

	if cfg.ListenAddr != ":9090" {
		t.Fatalf("expected ListenAddr=:9090, got %v", cfg.ListenAddr)
	}
}

func TestParseConfig_DefaultListenAddr(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"static-server", "--networks=mainnet"}

	cfg := parseConfig()

	if cfg.ListenAddr != ":8080" {
		t.Fatalf("expected default ListenAddr=:8080, got %v", cfg.ListenAddr)
	}
}

func TestParseConfig_ListenAddrFromEnv(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	t.Setenv("LISTEN_ADDR", ":7070")
	os.Args = []string{"static-server", "--networks=mainnet"}

	cfg := parseConfig()

	if cfg.ListenAddr != ":7070" {
		t.Fatalf("expected ListenAddr=:7070 from env, got %v", cfg.ListenAddr)
	}
}

// ---------------------------------------------------------------------------
// Metadata fetch tests
// ---------------------------------------------------------------------------

func TestFetchMetadata_Success(t *testing.T) {
	state := NetworkState{
		Network: "mainnet",
		Assignment: Assignment{
			FBURL: "http://example.com/data.fb.1.gz",
			ID:    "test-id-123",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()

	got, err := fetchMetadata(context.Background(), srv.Client(), "mainnet", srv.URL)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if got.Network != "mainnet" {
		t.Fatalf("expected network=mainnet, got %q", got.Network)
	}
	if got.Assignment.ID != "test-id-123" {
		t.Fatalf("expected assignment.id=test-id-123, got %q", got.Assignment.ID)
	}
	if got.Assignment.FBURL != "http://example.com/data.fb.1.gz" {
		t.Fatalf("expected fb_url_v1=http://example.com/data.fb.1.gz, got %q", got.Assignment.FBURL)
	}
}

func TestFetchMetadata_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := fetchMetadata(context.Background(), srv.Client(), "mainnet", srv.URL)
	if err == nil {
		t.Fatal("expected error for server 500, got nil")
	}
}

func TestFetchMetadata_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	_, err := fetchMetadata(context.Background(), srv.Client(), "mainnet", srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestFetchMetadata_MissingFBURL(t *testing.T) {
	state := NetworkState{
		Network: "mainnet",
		Assignment: Assignment{
			FBURL: "",
			ID:    "test-id-123",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()

	_, err := fetchMetadata(context.Background(), srv.Client(), "mainnet", srv.URL)
	if err == nil {
		t.Fatal("expected error for missing fb_url_v1, got nil")
	}
}

// ---------------------------------------------------------------------------
// Polling loop tests
// ---------------------------------------------------------------------------

func TestPollingLoop_DetectsChange(t *testing.T) {
	cacheDir := t.TempDir()
	var requestCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve data file requests
		if strings.HasPrefix(r.URL.Path, "/data/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content"))
			return
		}

		// Serve metadata -- vary assignment ID by request count
		count := requestCount.Add(1)
		var id string
		if count <= 1 {
			id = "assignment-A"
		} else {
			id = "assignment-B"
		}

		state := NetworkState{
			Network: "testnet",
			Assignment: Assignment{
				FBURL: fmt.Sprintf("%s/data/%s.fb.1.gz", "SERVERURL", id),
				ID:    id,
			},
		}
		// Replace SERVERURL placeholder -- we don't know srv.URL at definition time
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(state)
		bodyStr := strings.ReplaceAll(string(body), "SERVERURL", "http://"+r.Host)
		w.Write([]byte(bodyStr))
	}))
	defer srv.Close()

	cfg := Config{
		Networks:     []string{"testnet"},
		PollInterval: 100 * time.Millisecond,
		CacheDir:     cacheDir,
		BaseURL:      srv.URL,
		IdleTimeout:  5 * time.Second,
	}

	cache := NewNetworkCache()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	runPoller(ctx, cfg, srv.Client(), cache, "testnet")

	// After two polls with different IDs, the second file should exist
	pathB := filepath.Join(cacheDir, "testnet", "assignment-B.fb.1.gz")
	if _, err := os.Stat(pathB); os.IsNotExist(err) {
		t.Fatalf("expected file for assignment-B at %s, but it does not exist", pathB)
	}
}

func TestPollingLoop_SkipsUnchanged(t *testing.T) {
	cacheDir := t.TempDir()
	var downloadCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track data file downloads
		if strings.HasPrefix(r.URL.Path, "/data/") {
			downloadCount.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content"))
			return
		}

		// Always return same assignment ID
		state := NetworkState{
			Network: "testnet",
			Assignment: Assignment{
				FBURL: fmt.Sprintf("http://%s/data/same.fb.1.gz", r.Host),
				ID:    "assignment-A",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()

	cfg := Config{
		Networks:     []string{"testnet"},
		PollInterval: 100 * time.Millisecond,
		CacheDir:     cacheDir,
		BaseURL:      srv.URL,
		IdleTimeout:  5 * time.Second,
	}

	cache := NewNetworkCache()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	runPoller(ctx, cfg, srv.Client(), cache, "testnet")

	// Should only download once (first poll), not on subsequent polls with same ID
	count := downloadCount.Load()
	if count != 1 {
		t.Fatalf("expected exactly 1 download, got %d", count)
	}
}

func TestPollingLoop_ContinuesOnFetchError(t *testing.T) {
	cacheDir := t.TempDir()
	var requestCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve data file requests
		if strings.HasPrefix(r.URL.Path, "/data/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content"))
			return
		}

		// First metadata request fails, second succeeds
		count := requestCount.Add(1)
		if count <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		state := NetworkState{
			Network: "testnet",
			Assignment: Assignment{
				FBURL: fmt.Sprintf("http://%s/data/recovered.fb.1.gz", r.Host),
				ID:    "assignment-recovered",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()

	cfg := Config{
		Networks:     []string{"testnet"},
		PollInterval: 100 * time.Millisecond,
		CacheDir:     cacheDir,
		BaseURL:      srv.URL,
		IdleTimeout:  5 * time.Second,
	}

	cache := NewNetworkCache()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	runPoller(ctx, cfg, srv.Client(), cache, "testnet")

	// After the second poll succeeds, the file should exist
	path := filepath.Join(cacheDir, "testnet", "assignment-recovered.fb.1.gz")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected file at %s after recovery from error, but it does not exist", path)
	}
}

func TestPollingLoop_DeletesOldFile(t *testing.T) {
	cacheDir := t.TempDir()
	var requestCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve data file requests
		if strings.HasPrefix(r.URL.Path, "/data/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content"))
			return
		}

		// First poll returns "A", second returns "B"
		count := requestCount.Add(1)
		var id string
		if count <= 1 {
			id = "assignment-A"
		} else {
			id = "assignment-B"
		}

		state := NetworkState{
			Network: "testnet",
			Assignment: Assignment{
				FBURL: fmt.Sprintf("http://%s/data/%s.fb.1.gz", r.Host, id),
				ID:    id,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()

	cfg := Config{
		Networks:     []string{"testnet"},
		PollInterval: 100 * time.Millisecond,
		CacheDir:     cacheDir,
		BaseURL:      srv.URL,
		IdleTimeout:  5 * time.Second,
	}

	cache := NewNetworkCache()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	runPoller(ctx, cfg, srv.Client(), cache, "testnet")

	// File for assignment A should have been deleted after B was downloaded
	pathA := filepath.Join(cacheDir, "testnet", "assignment-A.fb.1.gz")
	if _, err := os.Stat(pathA); !os.IsNotExist(err) {
		t.Fatalf("expected file for assignment-A to be deleted, but it still exists at %s", pathA)
	}

	// File for assignment B should exist
	pathB := filepath.Join(cacheDir, "testnet", "assignment-B.fb.1.gz")
	if _, err := os.Stat(pathB); os.IsNotExist(err) {
		t.Fatalf("expected file for assignment-B at %s, but it does not exist", pathB)
	}
}

func TestPollingLoop_UpdatesCache(t *testing.T) {
	cacheDir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve data file requests
		if strings.HasPrefix(r.URL.Path, "/data/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content"))
			return
		}

		// Always return a stable assignment
		state := NetworkState{
			Network: "testnet",
			Assignment: Assignment{
				FBURL: fmt.Sprintf("http://%s/data/assign-1.fb.1.gz", r.Host),
				ID:    "assign-1",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}))
	defer srv.Close()

	cfg := Config{
		Networks:     []string{"testnet"},
		PollInterval: 100 * time.Millisecond,
		CacheDir:     cacheDir,
		BaseURL:      srv.URL,
		IdleTimeout:  5 * time.Second,
	}

	cache := NewNetworkCache()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	runPoller(ctx, cfg, srv.Client(), cache, "testnet")

	// After runPoller returns, cache should have the entry
	entry := cache.Get("testnet")
	if entry == nil {
		t.Fatal("expected cache entry for testnet, got nil")
	}
	if entry.State.Assignment.ID != "assign-1" {
		t.Fatalf("expected assignment ID assign-1, got %q", entry.State.Assignment.ID)
	}
	if !strings.HasSuffix(entry.FilePath, "assign-1.fb.1.gz") {
		t.Fatalf("expected FilePath ending with assign-1.fb.1.gz, got %q", entry.FilePath)
	}
}
