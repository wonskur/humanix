package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFileRejectsHTTP(t *testing.T) {
	target := filepath.Join(t.TempDir(), "package.tar")
	if err := DownloadFile(context.Background(), "http://example.org/package.tar", target); err == nil {
		t.Fatal("expected non-HTTPS URL to be rejected")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should not be created, stat error: %v", err)
	}
}

func TestDownloadFileDoesNotReplaceTargetOnHTTPFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream error", http.StatusBadGateway)
	}))
	defer server.Close()

	previousClient := defaultClient
	client := *server.Client()
	defaultClient = &client
	defer func() { defaultClient = previousClient }()

	target := filepath.Join(t.TempDir(), "package.tar")
	if err := os.WriteFile(target, []byte("old package"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DownloadFile(context.Background(), server.URL+"/package.tar", target); err == nil {
		t.Fatal("expected non-200 response to fail")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old package" {
		t.Fatalf("target was modified after failed download: %q", content)
	}
}
