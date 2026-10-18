package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/utils"
)

func createMockDeb(t *testing.T, dir, pkgName, version string) string {
	t.Helper()

	// Control tar
	var ctrlBuf bytes.Buffer
	cgw := gzip.NewWriter(&ctrlBuf)
	ctw := tar.NewWriter(cgw)

	ctrlContent := fmt.Sprintf("Package: %s\nVersion: %s\nDescription: Install test\nDepends: bash\n", pkgName, version)
	_ = ctw.WriteHeader(&tar.Header{Name: "control", Mode: 0644, Size: int64(len(ctrlContent))})
	_, _ = ctw.Write([]byte(ctrlContent))
	_ = ctw.Close()
	_ = cgw.Close()

	// Data tar
	var dataBuf bytes.Buffer
	dgw := gzip.NewWriter(&dataBuf)
	dtw := tar.NewWriter(dgw)

	binData := "#!/bin/sh\necho test\n"
	_ = dtw.WriteHeader(&tar.Header{Name: "usr/bin/my-tool", Mode: 0755, Size: int64(len(binData))})
	_, _ = dtw.Write([]byte(binData))
	_ = dtw.Close()
	_ = dgw.Close()

	// AR file
	debPath := filepath.Join(dir, pkgName+".deb")
	f, err := os.Create(debPath)
	if err != nil {
		t.Fatalf("failed creating deb file: %v", err)
	}
	defer f.Close()

	_, _ = f.WriteString("!<arch>\n")
	writeEntry(f, "debian-binary", []byte("2.0\n"))
	writeEntry(f, "control.tar.gz", ctrlBuf.Bytes())
	writeEntry(f, "data.tar.gz", dataBuf.Bytes())

	return debPath
}

func writeEntry(w *os.File, name string, content []byte) {
	hdr := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", name, "0", "0", "0", "100644", len(content))
	_, _ = w.WriteString(hdr)
	_, _ = w.Write(content)
	if len(content)%2 != 0 {
		_, _ = w.WriteString("\n")
	}
}

func TestInstallForeignDeb(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UPM_ROOT", tmpDir)

	dbConn, err := db.Open(utils.DBPath())
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer dbConn.Close()

	database := db.New(dbConn)
	inst := New(database)

	debPath := createMockDeb(t, tmpDir, "my-tool-pkg", "1.0.0")

	res, err := inst.InstallForeign(context.Background(), nil, debPath)
	if err != nil {
		t.Fatalf("InstallForeign failed: %v", err)
	}

	if res.Package.Name != "my-tool-pkg" {
		t.Errorf("expected package name 'my-tool-pkg', got %q", res.Package.Name)
	}

	// 1. Verify files exist in the isolated package directory.
	optTarget := filepath.Join(utils.ProviderOptDir("debian", "my-tool-pkg"), "usr", "bin", "my-tool")
	if _, err := os.Stat(optTarget); err != nil {
		t.Errorf("binary not found in opt directory: %v", err)
	}

	// 2. Verify wrapper created in /usr/bin/my-tool
	wrapperPath := filepath.Join(utils.BinDir(), "my-tool")
	if _, err := os.Stat(wrapperPath); err != nil {
		t.Errorf("wrapper script not found in bin directory: %v", err)
	}

	// 3. Verify database registration
	pkgInDB, err := database.GetInstalled(context.Background(), "my-tool-pkg")
	if err != nil {
		t.Fatalf("package not found in database: %v", err)
	}
	if pkgInDB.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", pkgInDB.Version)
	}
	if pkgInDB.Provider != "debian" {
		t.Errorf("expected provider debian, got %s", pkgInDB.Provider)
	}

	// 4. Verify file ownership query
	owner, err := database.FindOwner(context.Background(), filepath.ToSlash(wrapperPath))
	if err != nil {
		t.Fatalf("failed finding wrapper owner: %v", err)
	}
	if owner != "my-tool-pkg" {
		t.Errorf("expected owner 'my-tool-pkg', got %q", owner)
	}
}

func TestInstallForeignRefusesCommandCollision(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UPM_ROOT", tmpDir)
	dbConn, err := db.Open(utils.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer dbConn.Close()

	binDir := utils.BinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(binDir, "my-tool")
	if err := os.WriteFile(commandPath, []byte("user command"), 0755); err != nil {
		t.Fatal(err)
	}
	archivePath := createMockDeb(t, tmpDir, "collision-pkg", "1.0.0")
	installer := New(db.New(dbConn))
	if _, err := installer.InstallForeign(context.Background(), nil, archivePath); err == nil {
		t.Fatal("expected existing command collision to fail")
	}
	content, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "user command" {
		t.Fatalf("existing command was modified: %q", content)
	}
	if _, err := os.Stat(utils.ProviderOptDir("debian", "collision-pkg")); !os.IsNotExist(err) {
		t.Fatalf("package directory should not be activated after collision: %v", err)
	}
}
