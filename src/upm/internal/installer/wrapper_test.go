package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wonskur/upm/internal/utils"
)

func TestGenerateBwrapWrapper(t *testing.T) {
	script, err := GenerateBwrapWrapper("fedora", "htop", "htop", "/usr/bin/htop")
	if err != nil {
		t.Fatalf("GenerateBwrapWrapper failed: %v", err)
	}

	optDir := filepath.Join(utils.RuntimeOptDir(), "fedora", "htop")
	checks := []string{
		"#!/bin/sh",
		"BWRAP_BIN",
		shellQuote(optDir),
		`--setenv TERM "${TERM:-xterm}"`,
		"--setenv LD_LIBRARY_PATH",
		`"$TARGET_BIN" "$@"`,
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("expected script to contain %q, but got:\n%s", check, script)
		}
	}
	if !strings.Contains(script, filepath.Join(optDir, "usr", "bin", "htop")) {
		t.Errorf("wrapper did not include expected target path: %s", script)
	}
}

func TestCreateWrapperFile(t *testing.T) {
	tmpDir := t.TempDir()
	binPath, err := CreateWrapperFile(tmpDir, "debian", "curl", "curl", "/usr/bin/curl")
	if err != nil {
		t.Fatalf("CreateWrapperFile failed: %v", err)
	}

	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("failed to stat wrapper file: %v", err)
	}

	// On unix mode includes 0755; check that file exists and has size > 0
	if info.Size() == 0 {
		t.Errorf("wrapper file is empty")
	}

	content, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("failed reading wrapper: %v", err)
	}

	optDir := filepath.Join(utils.RuntimeOptDir(), "debian", "curl")
	if !strings.Contains(string(content), optDir) {
		t.Errorf("wrapper content missing target path: %s", string(content))
	}
}

func TestGenerateBwrapWrapperInvalid(t *testing.T) {
	_, err := GenerateBwrapWrapper("", "pkg", "bin", "/bin/bin")
	if err == nil {
		t.Errorf("expected error for empty provider, got nil")
	}

	_, err = GenerateBwrapWrapper("fedora", "", "bin", "/bin/bin")
	if err == nil {
		t.Errorf("expected error for empty pkgName, got nil")
	}
	if _, err := GenerateBwrapWrapper("fedora", "../escape", "bin", "/usr/bin/bin"); err == nil {
		t.Error("expected unsafe package name to be rejected")
	}
	if _, err := GenerateBwrapWrapper("fedora", "pkg", "bin", "/../../etc/passwd"); err == nil {
		t.Error("expected unsafe target path to be rejected")
	}
	if _, err := GenerateBwrapWrapper("fedora", "pkg", "passwd", "/etc/passwd"); err == nil {
		t.Error("expected non-executable target path to be rejected")
	}
	if _, err := GenerateBwrapWrapper("fedora", "pkg", "bin", "/"); err == nil {
		t.Error("expected root target path to be rejected")
	}
}
