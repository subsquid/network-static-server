package main

import "sync"

// NetworkEntry holds the cached state and file path for a single network.
type NetworkEntry struct {
	State    *NetworkState
	FilePath string
}

// NetworkCache is a concurrent-safe cache of per-network entries.
// Pollers write via Set; HTTP handlers read via Get.
type NetworkCache struct {
	mu      sync.RWMutex
	entries map[string]*NetworkEntry
}

// NewNetworkCache creates an empty NetworkCache.
func NewNetworkCache() *NetworkCache {
	return &NetworkCache{entries: make(map[string]*NetworkEntry)}
}

// Get returns a copy of the entry for the given network, or nil if not cached.
// The caller receives an independent copy so it does not hold the read lock
// during subsequent I/O.
func (nc *NetworkCache) Get(network string) *NetworkEntry {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	entry := nc.entries[network]
	if entry == nil {
		return nil
	}
	// Shallow copy the entry and State so the caller gets an independent copy.
	// Strings are immutable in Go, so shallow copy is safe.
	stateCopy := *entry.State
	return &NetworkEntry{
		State:    &stateCopy,
		FilePath: entry.FilePath,
	}
}

// Set stores or replaces the entry for the given network.
func (nc *NetworkCache) Set(network string, state *NetworkState, filePath string) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	nc.entries[network] = &NetworkEntry{State: state, FilePath: filePath}
}
