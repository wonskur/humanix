package repo

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// RepoConfig defines a configured repository source.
type RepoConfig struct {
	Name     string
	Provider string
	URL      string
	Enabled  bool
}

// DefaultRepos returns standard baseline repositories when repos.conf is not found.
func DefaultRepos() []RepoConfig {
	return []RepoConfig{
		{
			Name:     "arch-core",
			Provider: "arch",
			URL:      DefaultArchCoreURL,
			Enabled:  true,
		},
		{
			Name:     "arch-extra",
			Provider: "arch",
			URL:      DefaultArchExtraURL,
			Enabled:  true,
		},
		{
			Name:     "fedora-base",
			Provider: "fedora",
			URL:      DefaultFedoraURL,
			Enabled:  true,
		},
	}
}

// LoadRepoConfigs parses an INI-style repos.conf file or returns defaults.
func LoadRepoConfigs(path string) ([]RepoConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultRepos(), nil
		}
		return nil, fmt.Errorf("failed to open repository config %s: %w", path, err)
	}
	defer file.Close()

	var repos []RepoConfig
	var current *RepoConfig

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil {
				repos = append(repos, *current)
			}
			sectionName := strings.Trim(line, "[]")
			current = &RepoConfig{
				Name:    sectionName,
				Enabled: true,
			}
			continue
		}

		if current == nil {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "provider":
			current.Provider = val
		case "url":
			current.URL = val
		case "enabled":
			current.Enabled = (strings.ToLower(val) == "true" || val == "1")
		}
	}

	if current != nil {
		repos = append(repos, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed reading repo config: %w", err)
	}

	return repos, nil
}
