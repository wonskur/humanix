package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuilder_MissingBuildScript(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := t.TempDir()

	b := New()
	_, err := b.BuildSource(context.Background(), tmpDir, destDir)
	if err == nil {
		t.Fatalf("expected error on missing build script, got nil")
	}
}

func TestBuilder_BuildSource(t *testing.T) {
	// Skip if no shell exists on test runner (e.g., bare Windows without sh)
	if _, err := exec.LookPath("sh"); err != nil {
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("skipping build execution test because sh/bash is not installed")
		}
	}

	tmpDir := t.TempDir()
	destDir := t.TempDir()

	// 1. Create version file
	_ = os.WriteFile(filepath.Join(tmpDir, "version"), []byte("my-custom-pkg 1.0.5\n"), 0644)

	// 2. Create build script
	buildScript := `#!/bin/sh
mkdir -p "$DESTDIR/usr/bin"
echo "hello from custom build" > "$DESTDIR/usr/bin/my-custom-bin"
chmod 755 "$DESTDIR/usr/bin/my-custom-bin"
`
	_ = os.WriteFile(filepath.Join(tmpDir, "build"), []byte(buildScript), 0755)

	b := New()
	res, err := b.BuildSource(context.Background(), tmpDir, destDir)
	if err != nil {
		t.Fatalf("BuildSource failed: %v", err)
	}

	if res.Package.Name != "my-custom-pkg" {
		t.Errorf("expected name 'my-custom-pkg', got %q", res.Package.Name)
	}
	if res.Package.Version != "1.0.5" {
		t.Errorf("expected version '1.0.5', got %q", res.Package.Version)
	}
	if res.Package.Provider != "local" {
		t.Errorf("expected provider 'local', got %q", res.Package.Provider)
	}

	if len(res.Files) == 0 {
		t.Errorf("expected non-empty files list")
	}
}
