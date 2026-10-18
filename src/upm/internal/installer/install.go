package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
)

// Installer manages installing packages into system paths or /opt/upm/<provider>/<pkg>.
type Installer struct {
	database *db.DB
}

// New creates a new Installer with the given database handle.
func New(database *db.DB) *Installer {
	return &Installer{
		database: database,
	}
}

// InstallForeign extracts a foreign package (.pkg.tar.zst, .deb, .rpm) into /opt/upm/<provider>/<pkg>/,
// generates bubblewrap sandboxing wrappers in /usr/bin, and registers metadata in SQLite.
func (i *Installer) InstallForeign(ctx context.Context, p *pkg.Package, archivePath string) (*pkg.ExtractionResult, error) {
	if _, err := os.Stat(archivePath); err != nil {
		return nil, fmt.Errorf("archive file not found %s: %w", archivePath, err)
	}

	provider := ""
	if p != nil && p.Provider != "" {
		provider = p.Provider
	}

	// 1. Unpack to temporary staging directory first
	stagingDir, err := os.MkdirTemp("", "upm-foreign-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	var result *pkg.ExtractionResult
	lowerPath := strings.ToLower(archivePath)

	if strings.HasSuffix(lowerPath, ".pkg.tar.zst") || strings.HasSuffix(lowerPath, ".pkg.tar.xz") || strings.HasSuffix(lowerPath, ".pkg.tar.gz") {
		result, err = pkg.ExtractArch(ctx, archivePath, stagingDir)
		if provider == "" {
			provider = "arch"
		}
	} else if strings.HasSuffix(lowerPath, ".deb") {
		result, err = pkg.ExtractDeb(ctx, archivePath, stagingDir)
		if provider == "" {
			provider = "debian"
		}
	} else if strings.HasSuffix(lowerPath, ".rpm") {
		result, err = pkg.ExtractRPM(ctx, archivePath, stagingDir)
		if provider == "" {
			provider = "fedora"
		}
	} else {
		// Fallback detection
		switch provider {
		case "debian":
			result, err = pkg.ExtractDeb(ctx, archivePath, stagingDir)
		case "fedora":
			result, err = pkg.ExtractRPM(ctx, archivePath, stagingDir)
		case "arch", "aur":
			result, err = pkg.ExtractArch(ctx, archivePath, stagingDir)
		default:
			return nil, fmt.Errorf("unrecognized package format for %s", archivePath)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed extracting package %s: %w", archivePath, err)
	}

	// Fill in provider metadata
	result.Package.Provider = provider
	if p != nil {
		if p.Name != "" {
			result.Package.Name = p.Name
		}
		if p.Version != "" {
			result.Package.Version = p.Version
		}
	}

	if result.Package.Name == "" {
		return nil, fmt.Errorf("failed to determine package name from archive %s", archivePath)
	}
	if !safeComponent.MatchString(provider) || !safeComponent.MatchString(result.Package.Name) {
		return nil, fmt.Errorf("invalid package identity: provider=%q name=%q", provider, result.Package.Name)
	}
	return i.installStagedForeign(ctx, result, stagingDir)
}

// InstallNative installs a native package directly into the target system root.
func (i *Installer) InstallNative(ctx context.Context, p *pkg.Package, rootDir string) error {
	if p == nil {
		return fmt.Errorf("cannot install nil package")
	}
	unlock, err := acquirePackageLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	p.InstallDate = time.Now().Unix()
	if i.database != nil {
		return i.database.RegisterInstalled(ctx, p, nil)
	}
	return nil
}

func moveOrCopyDir(src, dst string) error {
	// Try atomic rename first
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fallback to recursive copy if moving across filesystem boundaries
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		// Handle symlink
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(linkTarget, target)
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			_ = in.Close()
			return err
		}

		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		outCloseErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
		return outCloseErr
	})
}

func hashFile(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
