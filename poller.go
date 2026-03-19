package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// NetworkState represents the upstream metadata for a network.
type NetworkState struct {
	Network    string     `json:"network"`
	Assignment Assignment `json:"assignment"`
}

// Assignment represents a dataset assignment within a NetworkState.
type Assignment struct {
	FBURL         string `json:"fb_url_v1"`
	ID            string `json:"id"`
	EffectiveFrom int64  `json:"effective_from"`
}

// fetchMetadata fetches and parses the upstream metadata JSON for a network.
// baseURL allows test injection; in production it is "https://metadata.sqd-datasets.io".
func fetchMetadata(ctx context.Context, client *http.Client, network, baseURL string) (*NetworkState, error) {
	url := fmt.Sprintf("%s/network-state-%s.json", baseURL, network)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	var state NetworkState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	if state.Assignment.FBURL == "" {
		return nil, fmt.Errorf("missing fb_url_v1 in metadata for %s", network)
	}

	return &state, nil
}

// runPoller runs the polling loop for a single network. It polls upstream
// metadata at cfg.PollInterval, detects when assignment.id changes, downloads
// the new data file, and deletes the old one.
func runPoller(ctx context.Context, cfg Config, client *http.Client, cache *NetworkCache, network string) {
	logger := slog.With("network", network)
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	var lastAssignmentID string
	var currentFilePath string

	poll := func() {
		state, err := fetchMetadata(ctx, client, network, cfg.BaseURL)
		if err != nil {
			logger.Error("poll failed", "error", err)
			return
		}

		if state.Assignment.ID == lastAssignmentID {
			logger.Debug("assignment unchanged", "id", state.Assignment.ID)
			return
		}

		logger.Info("new assignment detected",
			"id", state.Assignment.ID,
			"prev_id", lastAssignmentID,
		)

		destPath := filepath.Join(cfg.CacheDir, network, state.Assignment.ID+".fb.1.gz")
		if err := downloadFile(ctx, client, state.Assignment.FBURL, destPath, cfg.IdleTimeout); err != nil {
			logger.Error("download failed", "error", err, "id", state.Assignment.ID)
			return
		}

		logger.Info("download complete", "id", state.Assignment.ID, "path", destPath)

		cache.Set(network, state, destPath)

		// Track old path before updating
		oldPath := currentFilePath
		currentFilePath = destPath
		lastAssignmentID = state.Assignment.ID

		// Delete previous file if it exists and differs from the new path
		if oldPath != "" && oldPath != destPath {
			if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
				logger.Warn("failed to remove old file", "path", oldPath, "error", err)
			}
		}
	}

	// Immediate first poll on start
	poll()

	for {
		select {
		case <-ctx.Done():
			logger.Info("poller shutting down")
			return
		case <-ticker.C:
			poll()
		}
	}
}
