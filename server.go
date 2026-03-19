package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Server handles HTTP requests for metadata and data files.
type Server struct {
	cfg    Config
	cache  *NetworkCache
	client *http.Client
}

// newServer creates a Server with the given configuration, cache, and HTTP client.
func newServer(cfg Config, cache *NetworkCache, client *http.Client) *Server {
	return &Server{cfg: cfg, cache: cache, client: client}
}

// routes registers HTTP handlers on a new ServeMux and returns it.
// The data route uses Go 1.22+ wildcard patterns. The metadata route
// is handled by the root handler because Go's ServeMux wildcards must be
// entire path segments, and /network-state-{name}.json embeds the variable
// within a segment. The root handler checks the path prefix and dispatches.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /data/{network}/{rest...}", s.handleDataFile)
	// The metadata pattern /network-state-{name}.json cannot use ServeMux
	// wildcards. Register at the root and filter by path prefix in the handler.
	// ServeMux longest-match guarantees /data/ routes are tried first.
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

// handleReady returns 200 when all networks have completed initial download,
// 503 otherwise. Used as a Kubernetes readiness probe.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if !s.cache.Ready() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok\n")
}

// handleRoot dispatches metadata requests matching /network-state-*.json
// and returns 404 for everything else.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/network-state-") && strings.HasSuffix(r.URL.Path, ".json") {
		s.handleMetadata(w, r)
		return
	}
	http.NotFound(w, r)
}

// handleMetadata serves network metadata JSON with fb_url_v1 rewritten to point
// at this cache service. If the cache has no entry for the network (data file
// not yet downloaded), the request is proxied to the upstream service unchanged.
func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract network name from /network-state-{name}.json
	path := r.URL.Path
	name := strings.TrimPrefix(path, "/network-state-")
	if !strings.HasSuffix(name, ".json") {
		http.NotFound(w, r)
		return
	}
	network := strings.TrimSuffix(name, ".json")
	if network == "" || strings.Contains(network, "/") {
		http.NotFound(w, r)
		return
	}

	entry := s.cache.Get(network)
	if entry == nil {
		// SERV-03: data file not ready -- proxy upstream unchanged
		s.proxyUpstream(w, r, network)
		return
	}

	// SERV-01: rewrite fb_url_v1 to point at our cache
	rewritten := *entry.State
	rewritten.Assignment.FBURL = fmt.Sprintf("http://%s/data/%s/%s.fb.1.gz",
		r.Host, network, entry.State.Assignment.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&rewritten)
}

// handleDataFile streams the cached data file from disk using http.ServeFile.
// Returns 404 if no cache entry exists for the network.
func (s *Server) handleDataFile(w http.ResponseWriter, r *http.Request) {
	network := r.PathValue("network")

	entry := s.cache.Get(network)
	if entry == nil || entry.FilePath == "" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, entry.FilePath)
}

// proxyUpstream forwards the metadata request to the upstream service unchanged.
// Used when the local cache does not yet have a data file for the network.
func (s *Server) proxyUpstream(w http.ResponseWriter, r *http.Request, network string) {
	url := fmt.Sprintf("%s/network-state-%s.json", s.cfg.BaseURL, network)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers from upstream to client
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
