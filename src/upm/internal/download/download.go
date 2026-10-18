package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const MaxDownloadSize = 1 << 30

var defaultClient = &http.Client{
	Timeout: 10 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-HTTPS URL")
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// DownloadFile downloads a bounded HTTPS response and atomically publishes it at destPath.
func DownloadFile(ctx context.Context, rawURL string, destPath string) error {
	parsedURL, parseErr := url.Parse(rawURL)
	if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return fmt.Errorf("refusing non-HTTPS download URL %q", rawURL)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", rawURL, err)
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download from %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error %d downloading %s", resp.StatusCode, rawURL)
	}

	if resp.ContentLength > MaxDownloadSize {
		return fmt.Errorf("download exceeds maximum size of %d bytes", MaxDownloadSize)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(destPath), ".upm-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary download file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0644); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to set download file permissions: %w", err)
	}

	written, err := io.Copy(tempFile, io.LimitReader(resp.Body, MaxDownloadSize+1))
	if err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write downloaded content: %w", err)
	}
	if written > MaxDownloadSize {
		tempFile.Close()
		return fmt.Errorf("download exceeds maximum size of %d bytes", MaxDownloadSize)
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to sync downloaded content: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close downloaded content: %w", err)
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		return fmt.Errorf("failed to publish downloaded content to %s: %w", destPath, err)
	}

	return nil
}
