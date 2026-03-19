package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleMetadata_RewritesFBURL(t *testing.T) {
	cache := NewNetworkCache(nil)
	state := &NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://upstream.example.com/original.fb.1.gz",
			ID:    "abc-123",
		},
	}
	cache.Set("testnet", state, "/tmp/testnet/abc-123.fb.1.gz")

	srv := newServer(Config{}, cache, http.DefaultClient)
	mux := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/network-state-testnet.json", nil)
	req.Host = "cache.local:8080"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", ct)
	}

	var got NetworkState
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expected := "http://cache.local:8080/data/testnet/abc-123.fb.1.gz"
	if got.Assignment.FBURL != expected {
		t.Fatalf("expected fb_url_v1=%q, got %q", expected, got.Assignment.FBURL)
	}
	if got.Network != "testnet" {
		t.Fatalf("expected network=testnet, got %q", got.Network)
	}
	if got.Assignment.ID != "abc-123" {
		t.Fatalf("expected assignment.id=abc-123, got %q", got.Assignment.ID)
	}
}

func TestHandleMetadata_ProxiesWhenNotReady(t *testing.T) {
	// Upstream server returns metadata unchanged
	upstreamState := NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://upstream.example.com/upstream-data.fb.1.gz",
			ID:    "upstream-id",
		},
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "true")
		json.NewEncoder(w).Encode(upstreamState)
	}))
	defer upstream.Close()

	cache := NewNetworkCache(nil) // empty -- no entries

	cfg := Config{BaseURL: upstream.URL}
	srv := newServer(cfg, cache, upstream.Client())
	mux := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/network-state-testnet.json", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from proxy, got %d", resp.StatusCode)
	}

	// Upstream headers should be forwarded
	if resp.Header.Get("X-Upstream") != "true" {
		t.Fatal("expected X-Upstream header from upstream to be forwarded")
	}

	var got NetworkState
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode proxied response: %v (body: %s)", err, string(body))
	}

	// fb_url_v1 should be unchanged (upstream value, not rewritten)
	if got.Assignment.FBURL != "http://upstream.example.com/upstream-data.fb.1.gz" {
		t.Fatalf("expected upstream fb_url_v1 unchanged, got %q", got.Assignment.FBURL)
	}
}

func TestHandleMetadata_ProxiesUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream error"))
	}))
	defer upstream.Close()

	cache := NewNetworkCache(nil) // empty

	cfg := Config{BaseURL: upstream.URL}
	srv := newServer(cfg, cache, upstream.Client())
	mux := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/network-state-testnet.json", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected status 502 from upstream error, got %d", resp.StatusCode)
	}
}

func TestHandleDataFile_Streams(t *testing.T) {
	// Create a temp file with known content
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testnet", "abc-123.fb.1.gz")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	content := "this-is-the-data-file-content-for-testing"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	cache := NewNetworkCache(nil)
	state := &NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://example.com/data.fb.1.gz",
			ID:    "abc-123",
		},
	}
	cache.Set("testnet", state, filePath)

	srv := newServer(Config{}, cache, http.DefaultClient)
	mux := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/data/testnet/abc-123.fb.1.gz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Fatalf("expected body=%q, got %q", content, string(body))
	}

	// Content-Length should be set by http.ServeFile
	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		t.Fatal("expected Content-Length header to be set")
	}
}

func TestHandleDataFile_NotFoundWhenNoCacheEntry(t *testing.T) {
	cache := NewNetworkCache(nil) // empty

	srv := newServer(Config{}, cache, http.DefaultClient)
	mux := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/data/testnet/abc-123.fb.1.gz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestRoutes_MethodRestriction(t *testing.T) {
	cache := NewNetworkCache(nil)
	srv := newServer(Config{}, cache, http.DefaultClient)
	mux := srv.routes()

	req := httptest.NewRequest(http.MethodPost, "/network-state-testnet.json", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405 for POST, got %d", resp.StatusCode)
	}
}
