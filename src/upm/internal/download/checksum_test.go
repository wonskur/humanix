package download_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wonskur/upm/internal/download"
)

func TestComputeSHA256(t *testing.T) {
	input := "humanix-pm-test"
	expected := "edaa5f80a02cd08b007decf2cd350b4fd449ed21954f222562106c2e61767166"

	hash, err := download.ComputeSHA256(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("ComputeSHA256 failed: %v", err)
	}

	if hash != expected {
		t.Errorf("Expected %s, got %s", expected, hash)
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "sample.txt")
	content := []byte("humanix-pm-test")

	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	expected := "edaa5f80a02cd08b007decf2cd350b4fd449ed21954f222562106c2e61767166"
	if err := download.VerifyFileSHA256(testFile, expected); err != nil {
		t.Errorf("VerifyFileSHA256 failed: %v", err)
	}

	if err := download.VerifyFileSHA256(testFile, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("Expected error on checksum mismatch, got nil")
	}
}
