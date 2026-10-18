package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/utils"
)

func TestInstaller_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UPM_ROOT", tmpDir)

	dbConn, err := db.Open(utils.DBPath())
	if err != nil {
		t.Fatalf("failed opening test db: %v", err)
	}
	defer dbConn.Close()

	database := db.New(dbConn)
	inst := New(database)

	// Create test file on disk
	binDir := utils.BinDir()
	_ = os.MkdirAll(binDir, 0755)
	dummyBin := filepath.Join(binDir, "app-to-remove")
	_ = os.WriteFile(dummyBin, []byte("#!/bin/sh\n"), 0755)
	checksum, err := hashFile(dummyBin)
	if err != nil {
		t.Fatalf("failed hashing test file: %v", err)
	}

	p := &pkg.Package{
		Name:     "removable-app",
		Version:  "1.0.0",
		Provider: "arch",
	}

	files := []pkg.ExtractedFileInfo{
		{
			Path:   filepath.ToSlash(dummyBin),
			SHA256: checksum,
			Mode:   0755,
		},
	}

	if err := database.RegisterInstalled(context.Background(), p, files); err != nil {
		t.Fatalf("failed registering package: %v", err)
	}

	// Verify installed
	if _, err := database.GetInstalled(context.Background(), "removable-app"); err != nil {
		t.Fatalf("package should be installed: %v", err)
	}

	// Remove
	if err := inst.Remove(context.Background(), "removable-app", false); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(dummyBin); !os.IsNotExist(err) {
		t.Errorf("file was not removed from disk: %s", dummyBin)
	}

	// Verify DB record is gone
	if _, err := database.GetInstalled(context.Background(), "removable-app"); err == nil {
		t.Errorf("package record still exists in database")
	}
}

func TestInstaller_RemoveWithDependents(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UPM_ROOT", tmpDir)

	dbConn, err := db.Open(utils.DBPath())
	if err != nil {
		t.Fatalf("failed opening test db: %v", err)
	}
	defer dbConn.Close()

	database := db.New(dbConn)
	inst := New(database)

	basePkg := &pkg.Package{Name: "base-lib", Version: "1.0", Provider: "humanix"}
	appPkg := &pkg.Package{Name: "app-tool", Version: "1.0", Provider: "humanix", Depends: []pkg.Dep{{Name: "base-lib"}}}

	_ = database.RegisterInstalled(context.Background(), basePkg, nil)
	_ = database.RegisterInstalled(context.Background(), appPkg, nil)

	// Attempt to remove base-lib without force -> must fail
	err = inst.Remove(context.Background(), "base-lib", false)
	if err == nil {
		t.Fatalf("expected error removing package required by another package, got nil")
	}
	if !strings.Contains(err.Error(), "required by") {
		t.Errorf("expected 'required by' in error, got: %v", err)
	}

	// Attempt with force=true -> must succeed
	if err := inst.Remove(context.Background(), "base-lib", true); err != nil {
		t.Fatalf("failed removing with force: %v", err)
	}
}
