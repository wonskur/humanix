package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/utils"
)

type wrapperPlan struct {
	name       string
	targetPath string
	script     string
	stagedPath string
	hash       string
}

func (i *Installer) installStagedForeign(ctx context.Context, result *pkg.ExtractionResult, stagingDir string) (*pkg.ExtractionResult, error) {
	unlock, err := acquirePackageLock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	optDir := utils.ProviderOptDir(result.Package.Provider, result.Package.Name)
	optParent := filepath.Dir(optDir)
	if err := os.MkdirAll(optParent, 0755); err != nil {
		return nil, fmt.Errorf("failed creating package parent directory: %w", err)
	}

	candidateDir, err := os.MkdirTemp(optParent, ".upm-new-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating package candidate directory: %w", err)
	}
	defer os.RemoveAll(candidateDir)
	if err := os.Remove(candidateDir); err != nil {
		return nil, err
	}
	if err := moveOrCopyDir(stagingDir, candidateDir); err != nil {
		return nil, fmt.Errorf("failed preparing package files: %w", err)
	}

	var oldFiles []pkg.ExtractedFileInfo
	oldOptDir := ""
	if i.database != nil {
		if oldPackage, getErr := i.database.GetInstalled(ctx, result.Package.Name); getErr == nil {
			oldFiles, err = i.database.GetFiles(ctx, result.Package.Name)
			if err != nil {
				return nil, fmt.Errorf("failed reading existing package files: %w", err)
			}
			if oldPackage.Provider != "waynux" && oldPackage.Provider != "local" {
				oldOptDir = utils.ProviderOptDir(oldPackage.Provider, oldPackage.Name)
				for _, file := range oldFiles {
					legacyPath := filepath.Join(utils.LegacyOptDir, oldPackage.Provider, oldPackage.Name)
					if pathWithin(legacyPath, filepath.Clean(file.Path)) {
						oldOptDir = legacyPath
						break
					}
				}
			}
		}
	}

	if _, err := os.Lstat(optDir); err == nil && oldOptDir != optDir {
		return nil, fmt.Errorf("target package directory already exists and is not registered: %s", optDir)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed checking target package directory: %w", err)
	}

	binDir := utils.BinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating wrapper directory: %w", err)
	}
	wrapperStageDir, err := os.MkdirTemp(binDir, ".upm-wrappers-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating wrapper staging directory: %w", err)
	}
	defer os.RemoveAll(wrapperStageDir)

	var wrappers []wrapperPlan
	seen := make(map[string]struct{})
	for _, candidate := range []string{"usr/bin", "bin", "usr/sbin", "sbin"} {
		dirPath := filepath.Join(candidateDir, candidate)
		entries, readErr := os.ReadDir(dirPath)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("failed reading package binary directory %s: %w", candidate, readErr)
		}
		for _, entry := range entries {
			binaryName := entry.Name()
			if !safeComponent.MatchString(binaryName) || binaryName == "." || binaryName == ".." {
				return nil, fmt.Errorf("unsafe executable name %q in package", binaryName)
			}
			binaryPath := filepath.Join(dirPath, binaryName)
			info, statErr := os.Stat(binaryPath)
			if statErr != nil {
				return nil, fmt.Errorf("failed inspecting package executable %s: %w", binaryName, statErr)
			}
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				continue
			}
			if _, exists := seen[binaryName]; exists {
				return nil, fmt.Errorf("package contains duplicate executable name %q", binaryName)
			}
			seen[binaryName] = struct{}{}

			targetPath := "/" + filepath.ToSlash(filepath.Join(candidate, binaryName))
			script, err := GenerateBwrapWrapper(result.Package.Provider, result.Package.Name, binaryName, targetPath)
			if err != nil {
				return nil, err
			}
			stagedPath := filepath.Join(wrapperStageDir, binaryName)
			if err := os.WriteFile(stagedPath, []byte(script), 0755); err != nil {
				return nil, fmt.Errorf("failed staging wrapper %s: %w", binaryName, err)
			}
			digest := sha256.Sum256([]byte(script))
			wrappers = append(wrappers, wrapperPlan{
				name:       binaryName,
				targetPath: filepath.Join(binDir, binaryName),
				script:     script,
				stagedPath: stagedPath,
				hash:       hex.EncodeToString(digest[:]),
			})
		}
	}

	oldWrapperPaths := make(map[string]struct{})
	for _, file := range oldFiles {
		path := filepath.Clean(file.Path)
		if filepath.Dir(path) == filepath.Clean(binDir) {
			oldWrapperPaths[path] = struct{}{}
		}
	}
	for _, wrapper := range wrappers {
		if _, err := os.Lstat(wrapper.targetPath); err == nil {
			owner := ""
			if i.database != nil {
				owner, err = i.database.FindOwner(ctx, filepath.ToSlash(wrapper.targetPath))
			}
			if err != nil || owner != result.Package.Name {
				return nil, fmt.Errorf("refusing to overwrite existing command %s", wrapper.targetPath)
			}
			oldWrapperPaths[wrapper.targetPath] = struct{}{}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed checking existing command %s: %w", wrapper.targetPath, err)
		}
	}

	wrapperBackupDir, err := os.MkdirTemp(binDir, ".upm-backup-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating wrapper rollback directory: %w", err)
	}
	defer os.RemoveAll(wrapperBackupDir)
	optBackupDir, err := os.MkdirTemp(optParent, ".upm-backup-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating package rollback directory: %w", err)
	}
	defer os.RemoveAll(optBackupDir)

	backups := make(map[string]string)
	var installedWrappers []string
	optInstalled := false
	oldOptBackedUp := false
	rollback := func() {
		for _, path := range installedWrappers {
			_ = os.Remove(path)
		}
		if optInstalled {
			_ = os.RemoveAll(optDir)
		}
		if oldOptBackedUp {
			_ = os.Rename(filepath.Join(optBackupDir, "old-package"), oldOptDir)
		}
		for original, backup := range backups {
			_ = os.Rename(backup, original)
		}
	}

	backupIndex := 0
	for path := range oldWrapperPaths {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			rollback()
			return nil, fmt.Errorf("failed inspecting previous wrapper %s: %w", path, err)
		}
		backup := filepath.Join(wrapperBackupDir, fmt.Sprintf("%d-%s", backupIndex, filepath.Base(path)))
		backupIndex++
		if err := os.Rename(path, backup); err != nil {
			rollback()
			return nil, fmt.Errorf("failed preserving existing wrapper %s: %w", path, err)
		}
		backups[path] = backup
	}

	if oldOptDir != "" {
		if _, err := os.Lstat(oldOptDir); err == nil {
			if err := os.Rename(oldOptDir, filepath.Join(optBackupDir, "old-package")); err != nil {
				rollback()
				return nil, fmt.Errorf("failed preserving old package directory: %w", err)
			}
			oldOptBackedUp = true
		} else if !os.IsNotExist(err) {
			rollback()
			return nil, fmt.Errorf("failed inspecting old package directory: %w", err)
		}
	}

	if err := os.Rename(candidateDir, optDir); err != nil {
		rollback()
		return nil, fmt.Errorf("failed activating package directory: %w", err)
	}
	optInstalled = true

	var installedFiles []pkg.ExtractedFileInfo
	for _, file := range result.Files {
		installedFiles = append(installedFiles, pkg.ExtractedFileInfo{
			Path:   filepath.ToSlash(filepath.Join(optDir, filepath.FromSlash(strings.TrimPrefix(file.Path, "/")))),
			SHA256: file.SHA256,
			Mode:   file.Mode,
			IsDir:  file.IsDir,
		})
	}
	for _, wrapper := range wrappers {
		if err := os.Link(wrapper.stagedPath, wrapper.targetPath); err != nil {
			rollback()
			return nil, fmt.Errorf("failed activating wrapper %s: %w", wrapper.name, err)
		}
		if err := os.Remove(wrapper.stagedPath); err != nil {
			_ = os.Remove(wrapper.targetPath)
			rollback()
			return nil, fmt.Errorf("failed finalizing wrapper %s: %w", wrapper.name, err)
		}
		installedWrappers = append(installedWrappers, wrapper.targetPath)
		installedFiles = append(installedFiles, pkg.ExtractedFileInfo{
			Path:   filepath.ToSlash(wrapper.targetPath),
			SHA256: wrapper.hash,
			Mode:   0755,
		})
	}

	result.Package.InstallDate = time.Now().Unix()
	if i.database != nil {
		if err := i.database.RegisterInstalled(ctx, &result.Package, installedFiles); err != nil {
			rollback()
			return nil, fmt.Errorf("failed registering installed package in database: %w", err)
		}
	}

	return result, nil
}
