package pkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func createTestDeb(t *testing.T, pkgName, version string) string {
	t.Helper()

	// 1. Build control.tar.gz
	var controlBuf bytes.Buffer
	gw := gzip.NewWriter(&controlBuf)
	tw := tar.NewWriter(gw)

	controlContent := fmt.Sprintf("Package: %s\nVersion: %s\nSection: utils\nPriority: optional\nArchitecture: amd64\nDepends: libc6 (>= 2.34), libssl3\nDescription: Test deb package\n Homepage: https://example.org\n", pkgName, version)
	ctrlHdr := &tar.Header{
		Name: "control",
		Mode: 0644,
		Size: int64(len(controlContent)),
	}
	if err := tw.WriteHeader(ctrlHdr); err != nil {
		t.Fatalf("failed writing control header: %v", err)
	}
	if _, err := tw.Write([]byte(controlContent)); err != nil {
		t.Fatalf("failed writing control content: %v", err)
	}
	_ = tw.Close()
	_ = gw.Close()

	// 2. Build data.tar.gz
	var dataBuf bytes.Buffer
	dgw := gzip.NewWriter(&dataBuf)
	dtw := tar.NewWriter(dgw)

	binContent := "#!/bin/sh\necho hello from deb\n"
	binHdr := &tar.Header{
		Name: "usr/bin/deb-app",
		Mode: 0755,
		Size: int64(len(binContent)),
	}
	if err := dtw.WriteHeader(binHdr); err != nil {
		t.Fatalf("failed writing data header: %v", err)
	}
	if _, err := dtw.Write([]byte(binContent)); err != nil {
		t.Fatalf("failed writing data content: %v", err)
	}
	_ = dtw.Close()
	_ = dgw.Close()

	// 3. Assemble AR archive (.deb)
	tmpDir := t.TempDir()
	debPath := filepath.Join(tmpDir, pkgName+".deb")
	debFile, err := os.Create(debPath)
	if err != nil {
		t.Fatalf("failed creating deb file: %v", err)
	}
	defer debFile.Close()

	// Global header
	_, _ = debFile.WriteString("!<arch>\n")

	// debian-binary
	debBin := "2.0\n"
	writeArEntry(t, debFile, "debian-binary", []byte(debBin))

	// control.tar.gz
	writeArEntry(t, debFile, "control.tar.gz", controlBuf.Bytes())

	// data.tar.gz
	writeArEntry(t, debFile, "data.tar.gz", dataBuf.Bytes())

	return debPath
}

func writeArEntry(t *testing.T, w *os.File, name string, content []byte) {
	t.Helper()
	// Format: name (16) mtime (12) uid (6) gid (6) mode (8) size (10) magic (2)
	hdr := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", name, "0", "0", "0", "100644", len(content))
	if _, err := w.WriteString(hdr); err != nil {
		t.Fatalf("failed writing ar header for %s: %v", name, err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("failed writing ar body for %s: %v", name, err)
	}
	if len(content)%2 != 0 {
		_, _ = w.WriteString("\n")
	}
}

func TestExtractDeb(t *testing.T) {
	debPath := createTestDeb(t, "sample-deb", "1.2.3")
	destDir := t.TempDir()

	res, err := ExtractDeb(context.Background(), debPath, destDir)
	if err != nil {
		t.Fatalf("ExtractDeb failed: %v", err)
	}

	if res.Package.Name != "sample-deb" {
		t.Errorf("expected name 'sample-deb', got %q", res.Package.Name)
	}
	if res.Package.Version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got %q", res.Package.Version)
	}
	if res.Package.Provider != "debian" {
		t.Errorf("expected provider 'debian', got %q", res.Package.Provider)
	}
	if len(res.Package.Depends) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(res.Package.Depends))
	}

	extractedFile := filepath.Join(destDir, "usr", "bin", "deb-app")
	content, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("failed reading extracted file: %v", err)
	}
	if string(content) != "#!/bin/sh\necho hello from deb\n" {
		t.Errorf("unexpected file content: %s", string(content))
	}
}
