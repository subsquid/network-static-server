package main

import (
	"sync"
	"testing"
)

func TestNetworkCache_GetReturnsNilForMissing(t *testing.T) {
	nc := NewNetworkCache()
	entry := nc.Get("nonexistent")
	if entry != nil {
		t.Fatalf("expected nil for missing network, got %+v", entry)
	}
}

func TestNetworkCache_SetAndGet(t *testing.T) {
	nc := NewNetworkCache()
	state := &NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://example.com/data.fb.1.gz",
			ID:    "abc-123",
		},
	}
	nc.Set("testnet", state, "/tmp/testnet/abc-123.fb.1.gz")

	entry := nc.Get("testnet")
	if entry == nil {
		t.Fatal("expected non-nil entry, got nil")
	}
	if entry.State.Network != "testnet" {
		t.Fatalf("expected network=testnet, got %q", entry.State.Network)
	}
	if entry.State.Assignment.ID != "abc-123" {
		t.Fatalf("expected assignment.id=abc-123, got %q", entry.State.Assignment.ID)
	}
	if entry.FilePath != "/tmp/testnet/abc-123.fb.1.gz" {
		t.Fatalf("expected FilePath=/tmp/testnet/abc-123.fb.1.gz, got %q", entry.FilePath)
	}
}

func TestNetworkCache_GetReturnsCopy(t *testing.T) {
	nc := NewNetworkCache()
	state := &NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://example.com/data.fb.1.gz",
			ID:    "abc-123",
		},
	}
	nc.Set("testnet", state, "/tmp/testnet/abc-123.fb.1.gz")

	entry := nc.Get("testnet")
	if entry == nil {
		t.Fatal("expected non-nil entry, got nil")
	}

	// Modify the returned entry
	entry.FilePath = "/modified/path"
	entry.State.Assignment.ID = "modified-id"

	// Original in cache should be unchanged
	original := nc.Get("testnet")
	if original == nil {
		t.Fatal("expected non-nil original entry, got nil")
	}
	if original.FilePath != "/tmp/testnet/abc-123.fb.1.gz" {
		t.Fatalf("cache entry was modified: FilePath=%q, want /tmp/testnet/abc-123.fb.1.gz", original.FilePath)
	}
	if original.State.Assignment.ID != "abc-123" {
		t.Fatalf("cache entry was modified: Assignment.ID=%q, want abc-123", original.State.Assignment.ID)
	}
}

func TestNetworkCache_OverwriteEntry(t *testing.T) {
	nc := NewNetworkCache()

	state1 := &NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://example.com/old.fb.1.gz",
			ID:    "old-id",
		},
	}
	nc.Set("testnet", state1, "/tmp/testnet/old-id.fb.1.gz")

	state2 := &NetworkState{
		Network: "testnet",
		Assignment: Assignment{
			FBURL: "http://example.com/new.fb.1.gz",
			ID:    "new-id",
		},
	}
	nc.Set("testnet", state2, "/tmp/testnet/new-id.fb.1.gz")

	entry := nc.Get("testnet")
	if entry == nil {
		t.Fatal("expected non-nil entry, got nil")
	}
	if entry.State.Assignment.ID != "new-id" {
		t.Fatalf("expected assignment.id=new-id after overwrite, got %q", entry.State.Assignment.ID)
	}
	if entry.FilePath != "/tmp/testnet/new-id.fb.1.gz" {
		t.Fatalf("expected FilePath=/tmp/testnet/new-id.fb.1.gz after overwrite, got %q", entry.FilePath)
	}
}

func TestNetworkCache_ConcurrentAccess(t *testing.T) {
	nc := NewNetworkCache()

	var wg sync.WaitGroup
	start := make(chan struct{})

	// 10 writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				state := &NetworkState{
					Network: "testnet",
					Assignment: Assignment{
						FBURL: "http://example.com/data.fb.1.gz",
						ID:    "id",
					},
				}
				nc.Set("testnet", state, "/tmp/testnet/file.fb.1.gz")
			}
		}(i)
	}

	// 100 reader goroutines
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				entry := nc.Get("testnet")
				// entry may be nil (before first Set) or non-nil, both are valid
				_ = entry
			}
		}()
	}

	close(start) // release all goroutines simultaneously
	wg.Wait()
}
