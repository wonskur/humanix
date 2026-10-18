package pkg

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// ExtractDeb unpacks a Debian package (.deb) into destDir and returns package metadata and extracted file list.
func ExtractDeb(ctx context.Context, srcPath, destDir string) (*ExtractionResult, error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open deb package %s: %w", srcPath, err)
	}
	defer file.Close()

	// Read AR global header: "!<arch>\n" (8 bytes)
	globalHeader := make([]byte, 8)
	if _, err := io.ReadFull(file, globalHeader); err != nil {
		return nil, fmt.Errorf("failed to read deb ar header: %w", err)
	}
	if string(globalHeader) != "!<arch>\n" {
		return nil, fmt.Errorf("invalid deb archive: missing !<arch> signature")
	}

	result := &ExtractionResult{
		Package: Package{
			Provider: "debian",
		},
	}

	headerBuf := make([]byte, 60)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		_, err := io.ReadFull(file, headerBuf)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed reading ar header: %w", err)
		}

		name := strings.TrimSpace(string(headerBuf[0:16]))
		name = strings.TrimSuffix(name, "/")
		sizeStr := strings.TrimSpace(string(headerBuf[48:58]))
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid entry size for %s: %w", name, err)
		}

		magic := string(headerBuf[58:60])
		if magic != "`\n" {
			return nil, fmt.Errorf("invalid ar entry header magic for %s", name)
		}

		entryReader := io.LimitReader(file, size)

		if strings.HasPrefix(name, "control.tar") {
			if err := extractDebControl(entryReader, name, &result.Package); err != nil {
				return nil, fmt.Errorf("failed extracting deb control metadata: %w", err)
			}
		} else if strings.HasPrefix(name, "data.tar") {
			files, err := extractDebData(ctx, entryReader, name, destDir)
			if err != nil {
				return nil, fmt.Errorf("failed extracting deb data: %w", err)
			}
			result.Files = append(result.Files, files...)
		} else {
			// Skip unknown entry (e.g., debian-binary)
			if _, err := io.Copy(io.Discard, entryReader); err != nil {
				return nil, fmt.Errorf("failed skipping ar entry %s: %w", name, err)
			}
		}

		// AR archive entries are 2-byte aligned
		if size%2 != 0 {
			if _, err := io.CopyN(io.Discard, file, 1); err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed reading ar padding: %w", err)
			}
		}
	}

	return result, nil
}

func getDecompressor(r io.Reader, filename string) (io.Reader, io.Closer, error) {
	if strings.HasSuffix(filename, ".gz") {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return gr, gr, nil
	} else if strings.HasSuffix(filename, ".xz") {
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return xr, nil, nil
	} else if strings.HasSuffix(filename, ".zst") {
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return zr, zr.IOReadCloser(), nil
	}
	// Uncompressed tar fallback
	return r, nil, nil
}

func extractDebControl(r io.Reader, filename string, p *Package) error {
	decomp, closer, err := getDecompressor(r, filename)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	tr := tar.NewReader(decomp)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name := filepath.Clean(hdr.Name)
		if name == "control" || name == "./control" {
			controlBytes, err := io.ReadAll(io.LimitReader(tr, 1024*1024))
			if err != nil {
				return err
			}
			parseDebControlData(string(controlBytes), p)
			break
		}
	}
	return nil
}

func extractDebData(ctx context.Context, r io.Reader, filename, destDir string) ([]ExtractedFileInfo, error) {
	decomp, closer, err := getDecompressor(r, filename)
	if err != nil {
		return nil, err
	}
	if closer != nil {
		defer closer.Close()
	}

	var extracted []ExtractedFileInfo
	tr := tar.NewReader(decomp)
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
			return nil, fmt.Errorf("failed reading data tar entry: %w", err)
		}
		if err := budget.add(hdr.Size); err != nil {
			return nil, fmt.Errorf("unsafe archive entry %s: %w", hdr.Name, err)
		}

		relPath, targetPath, err := safeArchivePath(destDir, hdr.Name)
		if err != nil {
			return nil, err
		}
		if relPath == "." || relPath == "" {
			continue
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return nil, fmt.Errorf("failed creating directory %s: %w", targetPath, err)
			}
			extracted = append(extracted, ExtractedFileInfo{
				Path:  "/" + filepath.ToSlash(relPath),
				Mode:  hdr.Mode,
				IsDir: true,
			})
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return nil, fmt.Errorf("failed creating parent directory for %s: %w", targetPath, err)
		}

		if hdr.Typeflag == tar.TypeSymlink {
			targetPath, err = safeArchiveSymlink(destDir, hdr.Name, hdr.Linkname)
			if err != nil {
				return nil, err
			}
			_ = os.Remove(targetPath)
			if err := os.Symlink(hdr.Linkname, targetPath); err != nil {
				return nil, fmt.Errorf("failed creating symlink %s -> %s: %w", targetPath, hdr.Linkname, err)
			}
			extracted = append(extracted, ExtractedFileInfo{
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
			return nil, fmt.Errorf("failed creating file %s: %w", targetPath, err)
		}

		hasher := sha256.New()
		w := io.MultiWriter(out, hasher)
		if _, err := io.Copy(w, tr); err != nil {
			out.Close()
			return nil, fmt.Errorf("failed writing file %s: %w", targetPath, err)
		}
		out.Close()

		extracted = append(extracted, ExtractedFileInfo{
			Path:   "/" + filepath.ToSlash(relPath),
			SHA256: hex.EncodeToString(hasher.Sum(nil)),
			Mode:   hdr.Mode,
		})
	}

	return extracted, nil
}

func parseDebControlData(content string, p *Package) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lastKey string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if lastKey == "Description" {
				p.Description += "\n" + strings.TrimSpace(line)
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		lastKey = key

		switch key {
		case "Package":
			p.Name = val
		case "Version":
			p.Version = val
		case "Description":
			p.Description = val
		case "Homepage":
			p.URL = val
		case "Depends":
			deps := strings.Split(val, ",")
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				name := dep
				ver := ""
				if idx := strings.Index(dep, "("); idx != -1 {
					name = strings.TrimSpace(dep[:idx])
					ver = strings.Trim(strings.TrimSpace(dep[idx:]), "()")
				}
				p.Depends = append(p.Depends, Dep{
					Name:    name,
					Version: ver,
					Type:    "runtime",
				})
			}
		}
	}
}
