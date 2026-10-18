package utils

import (
	"os"
	"path/filepath"
)

const (
	DefaultLibDir     = "/var/lib/upm"
	DefaultCacheDir   = "/var/cache/upm"
	DefaultOptDir     = "/opt/upm"
	DefaultEtcDir     = "/etc/upm"
	DefaultBinDir     = "/usr/bin"
	DefaultDBName     = "installed.db"
	DefaultConfigName = "repos.conf"
	LegacyLibDir      = "/var/lib/pm"
	LegacyCacheDir    = "/var/cache/pm"
	LegacyOptDir      = "/opt/pm"
	LegacyEtcDir      = "/etc/pm"
)

func userDataDir() string {
	if value := os.Getenv("XDG_DATA_HOME"); value != "" {
		return filepath.Join(value, "upm")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "upm")
	}
	return filepath.Join(os.TempDir(), "upm")
}

func userCacheDir() string {
	if value := os.Getenv("XDG_CACHE_HOME"); value != "" {
		return filepath.Join(value, "upm")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "upm")
	}
	return filepath.Join(os.TempDir(), "upm")
}

func userConfigDir() string {
	if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
		return filepath.Join(value, "upm")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "upm")
	}
	return filepath.Join(os.TempDir(), "upm")
}

func userStateDirFor(suffix string) string {
	switch suffix {
	case filepath.Join("var", "cache"):
		return userCacheDir()
	case "opt":
		return filepath.Join(userDataDir(), "opt")
	case filepath.Join("etc"):
		return userConfigDir()
	default:
		return userDataDir()
	}
}

// RootPrefix returns the optional sysroot prefix. PM_ROOT remains supported for compatibility.
func RootPrefix() string {
	if root := os.Getenv("UPM_ROOT"); root != "" {
		return root
	}
	return os.Getenv("PM_ROOT")
}

func stateDir(defaultDir, legacyDir, suffix string) string {
	if root := RootPrefix(); root != "" {
		newPath := filepath.Join(root, suffix, "upm")
		legacyPath := filepath.Join(root, suffix, "pm")
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
				return legacyPath
			}
		}
		return newPath
	}
	if err := os.MkdirAll(defaultDir, 0755); err == nil {
		return defaultDir
	}
	if _, legacyErr := os.Stat(legacyDir); legacyErr == nil {
		return legacyDir
	}
	return userStateDirFor(suffix)
}

// LibDir returns the directory containing the active package database.
func LibDir() string {
	newDir := DefaultLibDir
	legacyDir := LegacyLibDir
	if root := RootPrefix(); root != "" {
		newDir = filepath.Join(root, "var", "lib", "upm")
		legacyDir = filepath.Join(root, "var", "lib", "pm")
	}
	if _, err := os.Stat(filepath.Join(newDir, DefaultDBName)); err == nil {
		return newDir
	}
	if _, err := os.Stat(filepath.Join(legacyDir, DefaultDBName)); err == nil {
		return legacyDir
	}
	if err := os.MkdirAll(newDir, 0755); err == nil {
		return newDir
	}
	if _, legacyErr := os.Stat(legacyDir); legacyErr == nil {
		return legacyDir
	}
	return userDataDir()
}

// CacheDir returns the download and package cache directory.
func CacheDir() string {
	return stateDir(DefaultCacheDir, LegacyCacheDir, filepath.Join("var", "cache"))
}

// OptDir returns the installation directory for foreign packages.
func OptDir() string {
	return stateDir(DefaultOptDir, LegacyOptDir, "opt")
}

// EtcDir returns the package manager configuration directory.
func EtcDir() string {
	newDir := DefaultEtcDir
	legacyDir := LegacyEtcDir
	if root := RootPrefix(); root != "" {
		newDir = filepath.Join(root, "etc", "upm")
		legacyDir = filepath.Join(root, "etc", "pm")
	}
	if _, err := os.Stat(filepath.Join(newDir, DefaultConfigName)); err == nil {
		return newDir
	}
	if _, err := os.Stat(filepath.Join(legacyDir, DefaultConfigName)); err == nil {
		return legacyDir
	}
	if err := os.MkdirAll(newDir, 0755); err == nil {
		return newDir
	}
	if _, legacyErr := os.Stat(legacyDir); legacyErr == nil {
		return legacyDir
	}
	return userConfigDir()
}

// RuntimeOptDir returns the path wrappers use outside a sysroot installation.
func RuntimeOptDir() string {
	if RootPrefix() == "" {
		if err := os.MkdirAll(DefaultOptDir, 0755); err != nil {
			if _, legacyErr := os.Stat(LegacyOptDir); legacyErr == nil {
				return LegacyOptDir
			}
			return filepath.Join(userDataDir(), "opt")
		}
	}
	return DefaultOptDir
}

// BinDir returns the system binary directory for wrappers (/usr/bin).
func BinDir() string {
	if root := RootPrefix(); root != "" {
		return filepath.Join(root, "usr", "bin")
	}
	return DefaultBinDir
}

// DBPath returns the path to the installed packages SQLite database.
func DBPath() string {
	return filepath.Join(LibDir(), DefaultDBName)
}

// ConfigPath returns the path to the repository configuration file.
func ConfigPath() string {
	return filepath.Join(EtcDir(), DefaultConfigName)
}

// ProviderOptDir returns the path for a specific package in the selected opt directory.
func ProviderOptDir(provider, pkgName string) string {
	return filepath.Join(OptDir(), provider, pkgName)
}
