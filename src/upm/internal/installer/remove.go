package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wonskur/upm/internal/utils"
)

// Remove removes an installed package, its files, database records, and sandbox wrappers.
func (i *Installer) Remove(ctx context.Context, pkgName string, force bool) error {
	if i.database == nil {
		return fmt.Errorf("database not initialized")
	}
	unlock, err := acquirePackageLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	// 1. Check if package is installed
	p, err := i.database.GetInstalled(ctx, pkgName)
	if err != nil {
		return fmt.Errorf("cannot remove package: %w", err)
	}
	if !safeComponent.MatchString(p.Name) || !safeComponent.MatchString(p.Provider) {
		return fmt.Errorf("refusing to remove package with invalid identity: provider=%q name=%q", p.Provider, p.Name)
	}

	// 2. Check if other installed packages depend on this one
	if !force {
		dependents, err := i.database.GetDependents(ctx, pkgName)
		if err != nil {
			return fmt.Errorf("failed checking package dependents: %w", err)
		}
		if len(dependents) > 0 {
			return fmt.Errorf("cannot remove %s: required by %s (use --force to override)", pkgName, strings.Join(dependents, ", "))
		}
	}

	// 3. Fetch all installed files
	files, err := i.database.GetFiles(ctx, pkgName)
	if err != nil {
		return fmt.Errorf("failed fetching files for %s: %w", pkgName, err)
	}

	foreign := p.Provider != "waynux" && p.Provider != "humanix" && p.Provider != "local"
	optDir := ""
	if foreign {
		optDir = utils.ProviderOptDir(p.Provider, p.Name)
		legacyOptDir := filepath.Join(utils.LegacyOptDir, p.Provider, p.Name)
		for _, file := range files {
			if pathWithin(legacyOptDir, filepath.Clean(file.Path)) {
				optDir = legacyOptDir
				break
			}
		}
	}
	binDir := filepath.Clean(utils.BinDir())

	// Validate every recorded path before changing the filesystem.
	for _, file := range files {
		path := filepath.Clean(file.Path)
		if !filepath.IsAbs(path) || path != file.Path || path == "/" {
			return fmt.Errorf("refusing to remove unsafe recorded path %q", file.Path)
		}
		if foreign {
			if !pathWithin(optDir, path) && filepath.Dir(path) != binDir {
				return fmt.Errorf("refusing to remove foreign package path outside its roots: %s", path)
			}
		} else if !underSystemPath(path) {
			return fmt.Errorf("refusing to remove package path outside system locations: %s", path)
		}
		if file.SHA256 != "" {
			info, err := os.Lstat(path)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed inspecting owned path %s: %w", path, err)
			}
			if err == nil && info.Mode().IsRegular() {
				actual, err := hashFile(path)
				if err != nil {
					return fmt.Errorf("failed verifying owned file %s: %w", path, err)
				}
				if actual != file.SHA256 {
					return fmt.Errorf("refusing to remove modified file %s", path)
				}
			}
		}
	}

	var directories []string
	var movedPaths []string
	backups := make(map[string]string)
	var backupDirs []string
	var wrapperBackupDir string
	rollback := func() {
		for index := len(movedPaths) - 1; index >= 0; index-- {
			_ = os.Rename(backups[movedPaths[index]], movedPaths[index])
		}
		for _, backupDir := range backupDirs {
			_ = os.RemoveAll(backupDir)
		}
		if wrapperBackupDir != "" {
			_ = os.RemoveAll(wrapperBackupDir)
		}
	}

	if foreign {
		if _, err := os.Lstat(optDir); err == nil {
			backupDir, err := os.MkdirTemp(filepath.Dir(optDir), ".upm-remove-*")
			if err != nil {
				return fmt.Errorf("failed staging package removal: %w", err)
			}
			backupPath := filepath.Join(backupDir, "package")
			if err := os.Rename(optDir, backupPath); err != nil {
				_ = os.RemoveAll(backupDir)
				return fmt.Errorf("failed staging package directory removal: %w", err)
			}
			backups[optDir] = backupPath
			movedPaths = append(movedPaths, optDir)
			backupDirs = append(backupDirs, backupDir)
		}
	}

	for _, file := range files {
		path := filepath.Clean(file.Path)
		if foreign && pathWithin(optDir, path) {
			continue
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			rollback()
			return fmt.Errorf("failed inspecting owned path %s: %w", path, err)
		}
		if filepath.Dir(path) == binDir {
			if wrapperBackupDir == "" {
				wrapperBackupDir, err = os.MkdirTemp(binDir, ".upm-remove-*")
				if err != nil {
					rollback()
					return fmt.Errorf("failed staging wrapper removal: %w", err)
				}
			}
			backupPath := filepath.Join(wrapperBackupDir, fmt.Sprintf("%d-%s", len(movedPaths), filepath.Base(path)))
			if err := os.Rename(path, backupPath); err != nil {
				rollback()
				return fmt.Errorf("failed staging owned file %s: %w", path, err)
			}
			backups[path] = backupPath
			movedPaths = append(movedPaths, path)
			continue
		}
		backupDir, err := os.MkdirTemp(filepath.Dir(path), ".upm-remove-*")
		if err != nil {
			rollback()
			return fmt.Errorf("failed staging owned file %s: %w", path, err)
		}
		backupPath := filepath.Join(backupDir, filepath.Base(path))
		if err := os.Rename(path, backupPath); err != nil {
			_ = os.RemoveAll(backupDir)
			rollback()
			return fmt.Errorf("failed staging owned file %s: %w", path, err)
		}
		backups[path] = backupPath
		movedPaths = append(movedPaths, path)
		backupDirs = append(backupDirs, backupDir)
	}

	// Directories are removed only after the database transaction succeeds.
	for _, f := range files {
		if f.IsDir {
			directories = append(directories, f.Path)
		}
	}

	if err := i.database.DeleteInstalled(ctx, pkgName); err != nil {
		rollback()
		return fmt.Errorf("failed removing package %s from database: %w", pkgName, err)
	}

	for _, backupDir := range backupDirs {
		_ = os.RemoveAll(backupDir)
	}
	sort.Slice(directories, func(a, b int) bool { return len(directories[a]) > len(directories[b]) })
	for _, directory := range directories {
		_ = os.Remove(directory)
	}
	for _, file := range files {
		dir := filepath.Dir(file.Path)
		for dir != "/" && dir != "." && dir != filepath.Dir(dir) {
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) > 0 {
				break
			}
			_ = os.Remove(dir)
			dir = filepath.Dir(dir)
		}
	}
	if wrapperBackupDir != "" {
		_ = os.RemoveAll(wrapperBackupDir)
	}

	return nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func underSystemPath(path string) bool {
	for _, root := range []string{"/usr", "/etc", "/opt", "/var", "/bin", "/sbin", "/lib", "/lib64"} {
		if pathWithin(root, path) {
			return true
		}
	}
	return false
}
