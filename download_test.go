package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadFile_Success(t *testing.T) {
	// httptest server serves 1KB of data
	data := strings.Repeat("A", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(data))
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "output.dat")

	client := srv.Client()
	err := downloadFile(context.Background(), client, srv.URL+"/file", destPath, 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// File exists at destination with correct content
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(got) != data {
		t.Fatalf("content mismatch: got %d bytes, want %d bytes", len(got), len(data))
	}
}

func TestDownloadFile_AtomicSwap(t *testing.T) {
	data := "atomic-swap-test-data"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(data))
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "output.dat")

	client := srv.Client()
	err := downloadFile(context.Background(), client, srv.URL+"/file", destPath, 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// After download completes, destination path exists with correct content
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(got) != data {
		t.Fatalf("content mismatch: got %q, want %q", string(got), data)
	}

	// No .tmp files remain in the directory
	matches, err := filepath.Glob(filepath.Join(destDir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("temp files remain after download: %v", matches)
	}
}

func TestDownloadFile_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "output.dat")

	client := srv.Client()
	err := downloadFile(context.Background(), client, srv.URL+"/file", destPath, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for server 500, got nil")
	}

	// No file at destination
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatal("destination file should not exist after server error")
	}

	// No temp files remain
	matches, err := filepath.Glob(filepath.Join(destDir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("temp files remain after error: %v", matches)
	}
}

func TestDownloadFile_IdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send partial data then stall
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			w.Write([]byte("partial"))
			f.Flush()
		}
		// Block until the client disconnects (idle timeout will cancel the request).
		// Using r.Context().Done() avoids the 5s sleep in the server handler,
		// so the test completes quickly after the idle timeout fires.
		<-r.Context().Done()
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "output.dat")

	client := srv.Client()
	err := downloadFile(context.Background(), client, srv.URL+"/file", destPath, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error from idle timeout, got nil")
	}

	// No file at destination
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatal("destination file should not exist after idle timeout")
	}

	// No temp files remain
	matches, err := filepath.Glob(filepath.Join(destDir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("temp files remain after idle timeout: %v", matches)
	}
}

func TestDownloadFile_CreatesDirectories(t *testing.T) {
	data := "nested-dir-test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(data))
	}))
	defer srv.Close()

	destDir := t.TempDir()
	// Nested directories that don't exist yet
	destPath := filepath.Join(destDir, "a", "b", "c", "output.dat")

	client := srv.Client()
	err := downloadFile(context.Background(), client, srv.URL+"/file", destPath, 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// File exists at destination
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(got) != data {
		t.Fatalf("content mismatch: got %q, want %q", string(got), data)
	}
}

func TestDownloadFile_CleanupOnWriteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Send first chunk then block until client disconnects
		w.Write([]byte(strings.Repeat("X", 1024)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "output.dat")

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to simulate mid-download cancellation
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	client := srv.Client()
	err := downloadFile(ctx, client, srv.URL+"/file", destPath, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}

	// No file at destination
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatal("destination file should not exist after cancellation")
	}

	// No temp files remain
	matches, err := filepath.Glob(filepath.Join(destDir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("temp files remain after cancellation: %v", matches)
	}
}
