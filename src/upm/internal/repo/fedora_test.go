package repo_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"path/filepath"
	"testing"

	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/repo"
)

func createMockFedoraPrimaryXMLGz(t *testing.T) *bytes.Buffer {
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="2">
  <package type="rpm">
    <name>ripgrep</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="14.1.0" rel="1.fc39"/>
    <checksum type="sha256" pkgid="YES">a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890</checksum>
    <summary>Line-oriented search tool</summary>
    <description>ripgrep is a line-oriented search tool that recursively searches the current directory.</description>
    <url>https://github.com/BurntSushi/ripgrep</url>
    <time file="1700000000" build="1699999999"/>
    <format>
      <rpm:license>MIT OR Unlicense</rpm:license>
      <rpm:requires>
        <rpm:entry name="libc.so.6()(64bit)"/>
        <rpm:entry name="libm.so.6()(64bit)"/>
        <rpm:entry name="rpmlib(CompressedFileNames)" flags="LE" ver="3.0.4-1"/>
      </rpm:requires>
    </format>
  </package>
  <package type="rpm">
    <name>htop</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="3.3.0" rel="1.fc39"/>
    <checksum type="sha256" pkgid="YES">feedbeef12345678feedbeef12345678feedbeef12345678feedbeef12345678</checksum>
    <summary>Interactive process viewer</summary>
    <description>htop is an interactive process viewer for Unix systems.</description>
    <url>https://htop.dev</url>
    <format>
      <rpm:license>GPLv2+</rpm:license>
      <rpm:requires>
        <rpm:entry name="libncursesw.so.6()(64bit)"/>
      </rpm:requires>
    </format>
  </package>
</metadata>
`

	if _, err := gw.Write([]byte(xmlContent)); err != nil {
		t.Fatalf("Failed to write mock fedora XML: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}
	return buf
}

func TestParseFedoraPrimaryXML(t *testing.T) {
	buf := createMockFedoraPrimaryXMLGz(t)

	pkgs, err := repo.ParseFedoraPrimaryXML(buf)
	if err != nil {
		t.Fatalf("ParseFedoraPrimaryXML failed: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("Expected 2 packages, got %d", len(pkgs))
	}

	rg := pkgs[0]
	if rg.Name != "ripgrep" {
		t.Errorf("Expected name 'ripgrep', got '%s'", rg.Name)
	}
	if rg.Version != "14.1.0-1.fc39" {
		t.Errorf("Expected version '14.1.0-1.fc39', got '%s'", rg.Version)
	}
	if rg.Provider != "fedora" {
		t.Errorf("Expected provider 'fedora', got '%s'", rg.Provider)
	}
	if rg.License != "MIT OR Unlicense" {
		t.Errorf("Expected license 'MIT OR Unlicense', got '%s'", rg.License)
	}
	if rg.Checksum != "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890" {
		t.Errorf("Unexpected checksum: %s", rg.Checksum)
	}

	// rpmlib dependency should have been filtered out
	if len(rg.Depends) != 2 {
		t.Fatalf("Expected 2 non-rpmlib dependencies, got %d: %+v", len(rg.Depends), rg.Depends)
	}

	htop := pkgs[1]
	if htop.Name != "htop" {
		t.Errorf("Expected name 'htop', got '%s'", htop.Name)
	}
}

func TestFedoraDatabaseSyncAndSearch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_fedora.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer database.Close()

	d := db.New(database)
	ctx := context.Background()

	buf := createMockFedoraPrimaryXMLGz(t)
	pkgs, err := repo.ParseFedoraPrimaryXML(buf)
	if err != nil {
		t.Fatalf("ParseFedoraPrimaryXML failed: %v", err)
	}

	if err := d.SaveAvailableBatch(ctx, pkgs); err != nil {
		t.Fatalf("SaveAvailableBatch failed: %v", err)
	}

	results, err := d.SearchAvailable(ctx, "process viewer")
	if err != nil {
		t.Fatalf("SearchAvailable failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "htop" {
		t.Errorf("Expected search to find htop, got %+v", results)
	}
}
