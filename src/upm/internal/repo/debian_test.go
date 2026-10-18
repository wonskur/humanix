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

func createMockDebianPackagesGz(t *testing.T) *bytes.Buffer {
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)

	packagesContent := `Package: curl
Version: 7.88.1-10+deb12u5
Installed-Size: 432
Maintainer: Debian curl team
Architecture: amd64
Depends: libc6 (>= 2.34), libcurl4 (= 7.88.1-10+deb12u5), zlib1g (>= 1:1.1.4)
Section: web
Priority: standard
Homepage: https://curl.se
Description: command line tool for transferring data with URL syntax
 Curl is an open source command line tool and library for
 transferring data with URL syntax.
 .
 It supports HTTP, HTTPS, FTP, FTPS, and more.
Filename: pool/main/c/curl/curl_7.88.1-10+deb12u5_amd64.deb
Size: 212508
SHA256: 7d6c841ff51e51bcf5a45fa3c3d5e236526131451f22149b1ff56bb36a2ad124

Package: nano
Version: 7.2-1
Installed-Size: 840
Architecture: amd64
Depends: libc6 (>= 2.34), libncursesw6 (>= 6)
Homepage: https://www.nano-editor.org/
Description: small, friendly text editor inspired by Pico
 GNU nano is an easy-to-use text editor originally designed as a replacement
 for Pico.
SHA256: 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef
`

	if _, err := gw.Write([]byte(packagesContent)); err != nil {
		t.Fatalf("Failed to write mock debian content: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}
	return buf
}

func TestParseDebianPackages(t *testing.T) {
	buf := createMockDebianPackagesGz(t)

	pkgs, err := repo.ParseDebianPackages(buf)
	if err != nil {
		t.Fatalf("ParseDebianPackages failed: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("Expected 2 packages, got %d", len(pkgs))
	}

	curl := pkgs[0]
	if curl.Name != "curl" {
		t.Errorf("Expected name 'curl', got '%s'", curl.Name)
	}
	if curl.Version != "7.88.1-10+deb12u5" {
		t.Errorf("Expected version '7.88.1-10+deb12u5', got '%s'", curl.Version)
	}
	if curl.Provider != "debian" {
		t.Errorf("Expected provider 'debian', got '%s'", curl.Provider)
	}
	if curl.Checksum != "7d6c841ff51e51bcf5a45fa3c3d5e236526131451f22149b1ff56bb36a2ad124" {
		t.Errorf("Unexpected checksum: %s", curl.Checksum)
	}
	if len(curl.Depends) != 3 {
		t.Fatalf("Expected 3 dependencies for curl, got %d", len(curl.Depends))
	}
	if curl.Depends[0].Name != "libc6" || curl.Depends[0].Version != ">= 2.34" {
		t.Errorf("Unexpected dependency: %+v", curl.Depends[0])
	}

	nano := pkgs[1]
	if nano.Name != "nano" {
		t.Errorf("Expected name 'nano', got '%s'", nano.Name)
	}
}

func TestDebianDatabaseSyncAndSearch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_debian.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer database.Close()

	d := db.New(database)
	ctx := context.Background()

	buf := createMockDebianPackagesGz(t)
	pkgs, err := repo.ParseDebianPackages(buf)
	if err != nil {
		t.Fatalf("ParseDebianPackages failed: %v", err)
	}

	if err := d.SaveAvailableBatch(ctx, pkgs); err != nil {
		t.Fatalf("SaveAvailableBatch failed: %v", err)
	}

	results, err := d.SearchAvailable(ctx, "Pico")
	if err != nil {
		t.Fatalf("SearchAvailable failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "nano" {
		t.Errorf("Expected search to find nano, got %+v", results)
	}
}
