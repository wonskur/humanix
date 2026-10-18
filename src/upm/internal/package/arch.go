package pkg

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractArch unpacks an Arch Linux package (.pkg.tar.zst or .pkg.tar.gz) into destDir.
func ExtractArch(ctx context.Context, srcPath, destDir string) (*ExtractionResult, error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open arch package %s: %w", srcPath, err)
	}
	defer file.Close()

	decomp, closer, err := getDecompressor(file, srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress arch package: %w", err)
	}
	if closer != nil {
		defer closer.Close()
	}

	tr := tar.NewReader(decomp)
	result := &ExtractionResult{
		Package: Package{
			Provider: "arch",
		},
	}
	var budget extractionBudget

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed reading tar entry: %w", err)
		}
		if err := budget.add(hdr.Size); err != nil {
			return nil, fmt.Errorf("unsafe archive entry %s: %w", hdr.Name, err)
		}

		relPath, targetPath, err := safeArchivePath(destDir, hdr.Name)
		if err != nil {
			return nil, err
		}
		if relPath == "." {
			continue
		}

		// Parse metadata file .PKGINFO
		if relPath == ".PKGINFO" {
			infoBytes, _ := io.ReadAll(io.LimitReader(tr, 1024*1024))
			parseArchPkgInfo(string(infoBytes), &result.Package)
			continue
		}

		// Skip internal packaging metadata files
		if strings.HasPrefix(relPath, ".") {
			continue
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
			result.Files = append(result.Files, ExtractedFileInfo{
				Path:  "/" + filepath.ToSlash(relPath),
				Mode:  hdr.Mode,
				IsDir: true,
			})
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
		}

		if hdr.Typeflag == tar.TypeSymlink {
			targetPath, err = safeArchiveSymlink(destDir, hdr.Name, hdr.Linkname)
			if err != nil {
				return nil, err
			}
			_ = os.Remove(targetPath)
			if err := os.Symlink(hdr.Linkname, targetPath); err != nil {
				return nil, fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, hdr.Linkname, err)
			}
			result.Files = append(result.Files, ExtractedFileInfo{
				Path: "/" + filepath.ToSlash(relPath),
				Mode: hdr.Mode,
			})
			continue
		}

		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("unsupported archive entry type %d for %s", hdr.Typeflag, hdr.Name)
		}
		_ = os.Remove(targetPath)
		out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hdr.Mode)&0777)
		if err != nil {
			return nil, fmt.Errorf("failed to create file %s: %w", targetPath, err)
		}

		hasher := sha256.New()
		w := io.MultiWriter(out, hasher)
		if _, err := io.Copy(w, tr); err != nil {
			out.Close()
			return nil, fmt.Errorf("failed writing file %s: %w", targetPath, err)
		}
		out.Close()

		result.Files = append(result.Files, ExtractedFileInfo{
			Path:   "/" + filepath.ToSlash(relPath),
			SHA256: hex.EncodeToString(hasher.Sum(nil)),
			Mode:   hdr.Mode,
		})
	}

	return result, nil
}

func parseArchPkgInfo(content string, p *Package) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "pkgname":
			p.Name = val
		case "pkgver":
			p.Version = val
		case "pkgdesc":
			p.Description = val
		case "url":
			p.URL = val
		case "license":
			p.License = val
		case "depend":
			p.Depends = append(p.Depends, Dep{
				Name: val,
				Type: "runtime",
			})
		}
	}
}
