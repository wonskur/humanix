package utils

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDBPathFallsBackToUserStateWhenSystemRootIsNotWritable(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0755)

	home := t.TempDir()
	t.Setenv("UPM_ROOT", root)
	t.Setenv("PM_ROOT", "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	got := DBPath()
	want := filepath.Join(home, ".local", "share", "upm", DefaultDBName)
	if got != want {
		t.Fatalf("DBPath() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, filepath.Join(home, ".local", "share", "upm")) {
		t.Fatalf("DBPath() = %q, want user-local path under %q", got, filepath.Join(home, ".local", "share", "upm"))
	}
}

func TestDBPathFallsBackWhenExistingDBIsReadOnly(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "var", "lib", "upm")
	legacyDir := filepath.Join(root, "var", "lib", "pm")
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, DefaultDBName), []byte("readonly"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(newDir, 0555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(newDir, 0755)
	defer os.Chmod(legacyDir, 0755)

	home := t.TempDir()
	t.Setenv("UPM_ROOT", root)
	t.Setenv("PM_ROOT", "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	got := DBPath()
	want := filepath.Join(home, ".local", "share", "upm", DefaultDBName)
	if got != want {
		t.Fatalf("DBPath() = %q, want %q", got, want)
	}
}
