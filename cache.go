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
	mu       sync.RWMutex
	entries  map[string]*NetworkEntry
	networks []string
}

// NewNetworkCache creates a NetworkCache that expects the given networks.
// Ready returns true only once all expected networks have been cached.
func NewNetworkCache(networks []string) *NetworkCache {
	return &NetworkCache{
		entries:  make(map[string]*NetworkEntry),
		networks: networks,
	}
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

// Ready returns true when all expected networks have a cached entry.
func (nc *NetworkCache) Ready() bool {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	for _, n := range nc.networks {
		if nc.entries[n] == nil {
			return false
		}
	}
	return true
}

// Set stores or replaces the entry for the given network.
func (nc *NetworkCache) Set(network string, state *NetworkState, filePath string) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	nc.entries[network] = &NetworkEntry{State: state, FilePath: filePath}
}
