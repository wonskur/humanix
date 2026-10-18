package repo_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"path/filepath"
	"testing"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/repo"
)

func createMockArchDB(t *testing.T) *bytes.Buffer {
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	descContent := `%FILENAME%
bash-5.2.026-2-x86_64.pkg.tar.zst

%NAME%
bash

%BASE%
bash

%VERSION%
5.2.026-2

%DESC%
The GNU Bourne Again shell

%CSIZE%
1825838

%ISIZE%
8944510

%SHA256SUM%
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

%URL%
https://www.gnu.org/software/bash/bash.html

%LICENSE%
GPL-3.0-or-later

%ARCH%
x86_64

%BUILDDATE%
1709403814
`

	dependsContent := `%DEPENDS%
glibc
readline>=8.2
ncurses
`

	addFile := func(name string, body string) {
		data := []byte(body)
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader failed: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	addFile("bash-5.2.026-2/desc", descContent)
	addFile("bash-5.2.026-2/depends", dependsContent)

	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close failed: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gw.Close failed: %v", err)
	}

	return buf
}

func TestArchPackageURLs(t *testing.T) {
	urls := repo.ArchPackageURLs(pkg.Package{Name: "fastfetch", Version: "2.70.0-1"})
	if len(urls) == 0 {
		t.Fatal("ArchPackageURLs returned no URLs")
	}
	want := "https://geo.mirror.pkgbuild.com/extra/os/x86_64/fastfetch-2.70.0-1-x86_64.pkg.tar.zst"
	if urls[3] != want {
		t.Fatalf("expected extra repo zst URL %q, got %q", want, urls[3])
	}
}

func TestParseArchRepoDB(t *testing.T) {
	buf := createMockArchDB(t)

	pkgs, err := repo.ParseArchRepoDB(buf)
	if err != nil {
		t.Fatalf("ParseArchRepoDB failed: %v", err)
	}

	if len(pkgs) != 1 {
		t.Fatalf("Expected 1 package, got %d", len(pkgs))
	}

	p := pkgs[0]
	if p.Name != "bash" {
		t.Errorf("Expected name 'bash', got '%s'", p.Name)
	}
	if p.Version != "5.2.026-2" {
		t.Errorf("Expected version '5.2.026-2', got '%s'", p.Version)
	}
	if p.Provider != "arch" {
		t.Errorf("Expected provider 'arch', got '%s'", p.Provider)
	}
	if p.Description != "The GNU Bourne Again shell" {
		t.Errorf("Expected description 'The GNU Bourne Again shell', got '%s'", p.Description)
	}
	if p.Checksum != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("Unexpected checksum: %s", p.Checksum)
	}
	if len(p.Depends) != 3 {
		t.Fatalf("Expected 3 dependencies, got %d", len(p.Depends))
	}
	if p.Depends[1].Name != "readline" || p.Depends[1].Version != ">=8.2" {
		t.Errorf("Expected readline>=8.2, got %+v", p.Depends[1])
	}
}

func TestArchDatabaseSyncAndSearch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_arch.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer database.Close()

	d := db.New(database)
	ctx := context.Background()

	buf := createMockArchDB(t)
	pkgs, err := repo.ParseArchRepoDB(buf)
	if err != nil {
		t.Fatalf("ParseArchRepoDB failed: %v", err)
	}

	if err := d.SaveAvailableBatch(ctx, pkgs); err != nil {
		t.Fatalf("SaveAvailableBatch failed: %v", err)
	}

	count, err := d.CountAvailable(ctx)
	if err != nil {
		t.Fatalf("CountAvailable failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 available package in DB, got %d", count)
	}

	results, err := d.SearchAvailable(ctx, "Bourne")
	if err != nil {
		t.Fatalf("SearchAvailable failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "bash" {
		t.Errorf("Expected search to return bash, got %+v", results)
	}

	pkgInfo, err := d.GetAvailable(ctx, "bash", "arch")
	if err != nil {
		t.Fatalf("GetAvailable failed: %v", err)
	}
	if pkgInfo.Name != "bash" {
		t.Errorf("Expected bash, got %s", pkgInfo.Name)
	}
}
