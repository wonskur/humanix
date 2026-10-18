package builder

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wonskur/upm/internal/download"
	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/sandbox"
	"github.com/wonskur/upm/internal/utils"
)

// Builder orchestrates building packages from source manifests.
type Builder struct{}

// New creates a new Builder instance.
func New() *Builder {
	return &Builder{}
}

// BuildSource builds a package from sourceDir into destDir.
func (b *Builder) BuildSource(ctx context.Context, sourceDir, destDir string) (*pkg.ExtractionResult, error) {
	buildScript := filepath.Join(sourceDir, "build")
	if _, err := os.Stat(buildScript); err != nil {
		return nil, fmt.Errorf("build script not found in %s: %w", sourceDir, err)
	}

	pkgName := filepath.Base(sourceDir)
	pkgVer := "1.0.0"

	// Read version file if present
	verPath := filepath.Join(sourceDir, "version")
	if verBytes, err := os.ReadFile(verPath); err == nil {
		lines := strings.Split(strings.TrimSpace(string(verBytes)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			parts := strings.Fields(lines[0])
			if len(parts) >= 2 {
				pkgName = parts[0]
				pkgVer = parts[1]
			} else {
				pkgVer = parts[0]
			}
		}
	}

	// 1. Process sources
	cacheDir := utils.CacheDir()
	sourcesFile := filepath.Join(sourceDir, "sources")
	if _, err := os.Stat(sourcesFile); err == nil {
		if err := b.downloadSources(ctx, sourcesFile, cacheDir); err != nil {
			return nil, fmt.Errorf("failed downloading sources: %w", err)
		}
	}

	// 2. Verify checksums
	checksumsFile := filepath.Join(sourceDir, "checksums")
	if _, err := os.Stat(checksumsFile); err == nil {
		if err := b.verifyChecksums(checksumsFile, cacheDir); err != nil {
			return nil, fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// 3. Prepare build environment
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating destDir: %w", err)
	}

	workDir, err := os.MkdirTemp("", "pm-build-*")
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary build workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 4. Run build script
	if err := b.executeBuild(ctx, sourceDir, buildScript, destDir, workDir, cacheDir); err != nil {
		return nil, fmt.Errorf("build execution failed: %w", err)
	}

	// 5. Collect installed files
	result := &pkg.ExtractionResult{
		Package: pkg.Package{
			Name:        pkgName,
			Version:     pkgVer,
			Provider:    "local",
			BuildDate:   time.Now().Unix(),
			InstallDate: time.Now().Unix(),
		},
	}

	err = filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == destDir {
			return nil
		}

		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return err
		}

		item := pkg.ExtractedFileInfo{
			Path:  "/" + filepath.ToSlash(rel),
			Mode:  int64(info.Mode()),
			IsDir: info.IsDir(),
		}

		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if h, err := hashFile(path); err == nil {
				item.SHA256 = h
			}
		}

		result.Files = append(result.Files, item)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed collecting installed files: %w", err)
	}

	return result, nil
}

func (b *Builder) executeBuild(ctx context.Context, sourceDir, buildScript, destDir, workDir, cacheDir string) error {
	if _, err := sandbox.LookBwrap(); err != nil {
		return fmt.Errorf("refusing to run package build without bubblewrap isolation: %w", err)
	}

	cfg := sandbox.SandboxConfig{
		UnshareAll: true,
		RoBinds:    []string{sourceDir, "/usr", "/bin", "/lib", "/etc"},
		RwBinds:    []string{destDir, workDir},
		WorkDir:    workDir,
		Env: map[string]string{
			"DESTDIR": destDir,
			"PATH":    "/usr/bin:/bin:/usr/sbin:/sbin",
			"HOME":    workDir,
			"TMPDIR":  workDir,
			"LANG":    "C.UTF-8",
		},
	}
	if _, err := os.Stat("/lib64"); err == nil {
		cfg.RoBinds = append(cfg.RoBinds, "/lib64")
	}
	if _, err := os.Stat(cacheDir); err == nil {
		cfg.RoBinds = append(cfg.RoBinds, cacheDir)
	}

	return sandbox.Run(ctx, cfg, "/bin/sh", buildScript)
}

func (b *Builder) downloadSources(ctx context.Context, sourcesFile, cacheDir string) error {
	file, err := os.Open(sourcesFile)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dest := filepath.Join(cacheDir, filepath.Base(line))
		if _, err := os.Stat(dest); err == nil {
			// Already cached
			continue
		}
		if err := download.DownloadFile(ctx, line, dest); err != nil {
			return fmt.Errorf("failed downloading %s: %w", line, err)
		}
	}
	return scanner.Err()
}

func (b *Builder) verifyChecksums(checksumsFile, cacheDir string) error {
	file, err := os.Open(checksumsFile)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		expectedHash := parts[0]
		fileName := filepath.Base(parts[1])
		targetFile := filepath.Join(cacheDir, fileName)

		if err := download.VerifyFileSHA256(targetFile, expectedHash); err != nil {
			return fmt.Errorf("checksum mismatch for %s: %w", fileName, err)
		}
	}
	return scanner.Err()
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
