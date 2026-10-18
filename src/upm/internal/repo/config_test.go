package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wonskur/upm/internal/repo"
)

func TestLoadRepoConfigs_Default(t *testing.T) {
	tempDir := t.TempDir()
	nonExistent := filepath.Join(tempDir, "repos.conf")

	configs, err := repo.LoadRepoConfigs(nonExistent)
	if err != nil {
		t.Fatalf("Expected no error for missing config, got %v", err)
	}

	if len(configs) != 3 {
		t.Errorf("Expected two Arch and one Fedora foreign repo by default, got %d", len(configs))
	}
	if configs[0].Provider != "arch" {
		t.Errorf("Expected first default repo to be arch, got %s", configs[0].Provider)
	}
	for _, config := range configs {
		if config.Provider == "debian" {
			t.Error("Debian repository should not be enabled by default on a Debian-based system")
		}
	}
}

func TestLoadRepoConfigs_Custom(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "repos.conf")

	content := `
# Repositories Configuration
[custom-arch]
provider = arch
url = https://mirror.example.com/arch/core.db.tar.gz
enabled = true

[disabled-repo]
provider = arch
url = https://mirror.example.com/arch/extra.db.tar.gz
enabled = false
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	configs, err := repo.LoadRepoConfigs(configPath)
	if err != nil {
		t.Fatalf("LoadRepoConfigs failed: %v", err)
	}

	if len(configs) != 2 {
		t.Fatalf("Expected 2 repos, got %d", len(configs))
	}

	if configs[0].Name != "custom-arch" || !configs[0].Enabled || configs[0].URL != "https://mirror.example.com/arch/core.db.tar.gz" {
		t.Errorf("Unexpected config[0]: %+v", configs[0])
	}

	if configs[1].Name != "disabled-repo" || configs[1].Enabled {
		t.Errorf("Unexpected config[1]: %+v", configs[1])
	}
}
