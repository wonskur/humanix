package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxPackageFileSize     int64 = 2 << 30
	maxPackageExpandedSize int64 = 8 << 30
	maxPackageEntries            = 500_000
)

type extractionBudget struct {
	entries int
	bytes   int64
}

func (b *extractionBudget) add(size int64) error {
	if size < 0 || size > maxPackageFileSize {
		return fmt.Errorf("archive entry exceeds maximum file size")
	}
	b.entries++
	if b.entries > maxPackageEntries || size > maxPackageExpandedSize-b.bytes {
		return fmt.Errorf("archive exceeds extraction limits")
	}
	b.bytes += size
	return nil
}

func safeArchivePath(root, name string) (string, string, error) {
	if name == "" || strings.Contains(name, "\\") || filepath.IsAbs(name) {
		return "", "", fmt.Errorf("unsafe archive path %q", name)
	}

	rel := filepath.Clean(name)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.VolumeName(rel) != "" {
		return "", "", fmt.Errorf("unsafe archive path %q", name)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", "", err
	}
	if rel == "." {
		return rel, resolvedRoot, nil
	}
	current := resolvedRoot
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", "", fmt.Errorf("unsafe symlink in archive path %q: %w", name, err)
			}
			if !isWithin(resolvedRoot, resolved) {
				return "", "", fmt.Errorf("archive path %q escapes extraction root through symlink", name)
			}
			current = resolved
		} else if !info.IsDir() {
			return "", "", fmt.Errorf("non-directory component in archive path %q", name)
		}
	}

	return rel, filepath.Join(absRoot, rel), nil
}

func safeArchiveSymlink(root, name, linkname string) (string, error) {
	rel, target, err := safeArchivePath(root, name)
	if err != nil {
		return "", err
	}
	if linkname == "" || strings.Contains(linkname, "\\") || filepath.IsAbs(linkname) {
		return "", fmt.Errorf("unsafe symlink target %q for %q", linkname, name)
	}
	linkRel := filepath.Clean(filepath.Join(filepath.Dir(rel), linkname))
	if linkRel == ".." || strings.HasPrefix(linkRel, ".."+string(filepath.Separator)) || filepath.IsAbs(linkRel) {
		return "", fmt.Errorf("symlink %q escapes extraction root", name)
	}
	return target, nil
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
