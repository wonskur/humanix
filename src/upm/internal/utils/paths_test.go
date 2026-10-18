package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsPreferUPMRootAndKeepLegacyState(t *testing.T) {
	legacyRoot := t.TempDir()
	newRoot := t.TempDir()
	t.Setenv("PM_ROOT", legacyRoot)
	t.Setenv("UPM_ROOT", newRoot)

	if got, want := LibDir(), filepath.Join(newRoot, "var", "lib", "upm"); got != want {
		t.Fatalf("LibDir() = %q, want %q", got, want)
	}
	if got, want := OptDir(), filepath.Join(newRoot, "opt", "upm"); got != want {
		t.Fatalf("OptDir() = %q, want %q", got, want)
	}
}

func TestPathsFallBackToExistingLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "var", "lib", "pm")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, DefaultDBName), []byte("legacy db"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var", "lib", "upm"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PM_ROOT", root)
	t.Setenv("UPM_ROOT", "")

	if got := LibDir(); got != legacy {
		t.Fatalf("LibDir() = %q, want legacy path %q", got, legacy)
	}
}
