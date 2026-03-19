package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config holds the service configuration.
type Config struct {
	Networks     []string
	PollInterval time.Duration
	CacheDir     string
	BaseURL      string
	IdleTimeout  time.Duration
	ListenAddr   string
}

// envOrDefault returns the value of the environment variable key if non-empty,
// otherwise returns fallback.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envDurationOrDefault returns the duration parsed from the environment
// variable key. Returns fallback if the variable is empty or cannot be parsed.
func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// parseConfig parses configuration from CLI flags and environment variables.
// Flags take precedence over environment variables. If no networks are
// configured, the process exits with code 1.
func parseConfig() Config {
	networks := flag.String("networks", envOrDefault("NETWORKS", ""),
		"comma-separated list of network names to cache")
	pollInterval := flag.Duration("poll-interval", envDurationOrDefault("POLL_INTERVAL", 60*time.Second),
		"interval between upstream polls")
	cacheDir := flag.String("cache-dir", envOrDefault("CACHE_DIR", "/tmp/cache"),
		"directory for cached data files")
	listenAddr := flag.String("listen-addr", envOrDefault("LISTEN_ADDR", ":8080"),
		"HTTP server listen address")
	flag.Parse()

	if *networks == "" {
		slog.Error("no networks configured, use --networks or NETWORKS env var")
		os.Exit(1)
	}

	return Config{
		Networks:     strings.Split(*networks, ","),
		PollInterval: *pollInterval,
		CacheDir:     *cacheDir,
		BaseURL:      "https://metadata.sqd-datasets.io",
		IdleTimeout:  45 * time.Second,
		ListenAddr:   *listenAddr,
	}
}

func main() {
	// Set up structured JSON logging
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))

	cfg := parseConfig()
	slog.Info("starting static-server",
		"networks", cfg.Networks,
		"poll_interval", cfg.PollInterval.String(),
		"cache_dir", cfg.CacheDir,
		"listen_addr", cfg.ListenAddr,
	)

	cache := NewNetworkCache(cfg.Networks)

	// HTTP client with transport-level timeouts (no client.Timeout -- kills large downloads)
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           newServer(cfg, cache, client).routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		slog.Info("http server starting", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	// Signal-aware context cancels on SIGTERM/SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Start pollers
	var wg sync.WaitGroup
	for _, network := range cfg.Networks {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			runPoller(ctx, cfg, client, cache, name)
		}(network)
	}

	// Block until signal received
	<-ctx.Done()
	stop() // Restore default signal behavior -- second signal force-kills

	slog.Info("shutdown signal received")

	// Drain everything within 30s
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown HTTP server (stops accepting new connections, drains in-flight requests)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown error", "error", err)
	}

	// Wait for pollers to exit (they return when ctx is cancelled)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("shutdown complete")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timed out")
	}
}
