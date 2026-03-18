package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// idleTimeoutReader wraps an io.Reader and cancels the associated context
// if no data is received within the configured timeout duration. This detects
// stalled connections without penalizing slow-but-progressing large file downloads.
type idleTimeoutReader struct {
	reader  io.Reader
	cancel  context.CancelFunc
	timeout time.Duration
	timer   *time.Timer
}

// newIdleTimeoutReader creates an idleTimeoutReader that cancels the given
// cancel function when no data is received for the given timeout duration.
// The caller must call Close() when done to stop the watchdog goroutine.
func newIdleTimeoutReader(ctx context.Context, r io.Reader, timeout time.Duration, cancel context.CancelFunc) *idleTimeoutReader {
	itr := &idleTimeoutReader{
		reader:  r,
		cancel:  cancel,
		timeout: timeout,
		timer:   time.NewTimer(timeout),
	}

	// Watchdog goroutine: cancel context when idle timeout fires
	go func() {
		select {
		case <-itr.timer.C:
			cancel() // Idle timeout expired -- no data received
		case <-ctx.Done():
			// Normal completion or parent cancellation
		}
	}()

	return itr
}

// Read reads from the underlying reader and resets the idle timer on each
// successful read (n > 0). Uses the proper timer drain pattern to avoid races.
func (itr *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := itr.reader.Read(p)
	if n > 0 {
		// Data received -- reset idle timer
		if !itr.timer.Stop() {
			select {
			case <-itr.timer.C:
			default:
			}
		}
		itr.timer.Reset(itr.timeout)
	}
	return n, err
}

// Close stops the idle timer and cancels the derived context, which stops
// the watchdog goroutine.
func (itr *idleTimeoutReader) Close() {
	itr.timer.Stop()
	itr.cancel()
}

// downloadFile streams a file from url to destPath using an atomic temp-file-and-rename
// pattern. It creates any missing parent directories, uses an idle timeout reader to
// detect stalled connections, and cleans up the temp file on any failure.
func downloadFile(ctx context.Context, client *http.Client, url, destPath string, idleTimeout time.Duration) error {
	// Create a cancellable context that both the HTTP request and the idle
	// timeout reader share. When the idle timer fires, it cancels this context,
	// which also aborts the in-flight HTTP response body read.
	dlCtx, dlCancel := context.WithCancel(ctx)
	defer dlCancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}

	// Create target directory (same filesystem as temp file for atomic rename)
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Cleanup on any error -- skip cleanup on success
	success := false
	defer func() {
		if !success {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Wrap response body in idle timeout reader. The reader cancels dlCtx
	// on idle timeout, which also aborts the HTTP body read.
	reader := newIdleTimeoutReader(dlCtx, resp.Body, idleTimeout, dlCancel)
	defer reader.Close()

	if _, err := io.Copy(tmpFile, reader); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	// Flush to disk before rename
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	success = true
	return nil
}
