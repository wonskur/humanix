package repo

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
)

const (
	DefaultArchCoreURL  = "https://geo.mirror.pkgbuild.com/core/os/x86_64/core.db.tar.gz"
	DefaultArchExtraURL = "https://geo.mirror.pkgbuild.com/extra/os/x86_64/extra.db.tar.gz"
)

type ArchProvider struct {
	repoURLs []string
	database *db.DB
	client   *http.Client
}

// NewArchProvider creates a new Arch Linux repository provider.
func NewArchProvider(database *db.DB, repoURLs ...string) *ArchProvider {
	if len(repoURLs) == 0 {
		repoURLs = []string{DefaultArchCoreURL, DefaultArchExtraURL}
	}
	return &ArchProvider{
		repoURLs: repoURLs,
		database: database,
		client:   newSecureClient(120 * time.Second),
	}
}

func (a *ArchProvider) Name() string {
	return "arch"
}

// Sync downloads repository databases and updates the available table in SQLite.
func (a *ArchProvider) Sync(ctx context.Context) error {
	totalSynced := 0

	for _, rawURL := range a.repoURLs {
		if err := requireHTTPS(rawURL); err != nil {
			return err
		}
		slog.Info("syncing arch repository", "url", rawURL)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request for %s: %w", rawURL, err)
		}

		resp, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download arch repo from %s: %w", rawURL, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("HTTP error %d downloading %s", resp.StatusCode, rawURL)
		}

		limited := &io.LimitedReader{R: resp.Body, N: maxRepositoryDownload + 1}
		pkgs, err := ParseArchRepoDB(limited)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse arch repo database from %s: %w", rawURL, err)
		}
		if limited.N == 0 {
			return fmt.Errorf("repository metadata exceeds maximum size of %d bytes", maxRepositoryDownload)
		}

		if a.database != nil {
			if err := a.database.SaveAvailableBatch(ctx, pkgs); err != nil {
				return fmt.Errorf("failed to save arch packages to database: %w", err)
			}
		}

		slog.Info("arch repository synced", "url", rawURL, "packages", len(pkgs))
		totalSynced += len(pkgs)
	}

	slog.Info("arch provider sync completed", "total_packages", totalSynced)
	return nil
}

// Search searches available packages in database matching query.
func (a *ArchProvider) Search(ctx context.Context, query string) ([]pkg.Package, error) {
	if a.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	all, err := a.database.SearchAvailable(ctx, query)
	if err != nil {
		return nil, err
	}

	var filtered []pkg.Package
	for _, p := range all {
		if p.Provider == "arch" {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// Get finds a package by name in the Arch repository.
func (a *ArchProvider) Get(ctx context.Context, name string) (*pkg.Package, error) {
	if a.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return a.database.GetAvailable(ctx, name, "arch")
}

// ArchPackageURLs builds the candidate archive URLs for an Arch package from the core/extra repos.
func ArchPackageURLs(p pkg.Package) []string {
	if p.Name == "" || p.Version == "" {
		return nil
	}

	baseName := fmt.Sprintf("%s-%s-x86_64", p.Name, p.Version)
	archiveSuffixes := []string{".pkg.tar.zst", ".pkg.tar.xz", ".pkg.tar.gz"}
	repos := []string{"core", "extra"}

	urls := make([]string, 0, len(repos)*len(archiveSuffixes))
	for _, repo := range repos {
		for _, suffix := range archiveSuffixes {
			urls = append(urls, fmt.Sprintf("https://geo.mirror.pkgbuild.com/%s/os/x86_64/%s%s", repo, baseName, suffix))
		}
	}
	return urls
}

// ParseArchRepoDB parses an Arch Linux .db.tar.gz database stream into a slice of Package structs.
func ParseArchRepoDB(r io.Reader) ([]pkg.Package, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	pkgMap := make(map[string]*pkg.Package)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		// Security: prevent path traversal in db archive
		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
			continue
		}

		parts := strings.Split(header.Name, "/")
		if len(parts) < 2 {
			continue
		}
		pkgKey := parts[0]
		fileName := parts[1]

		p, exists := pkgMap[pkgKey]
		if !exists {
			p = &pkg.Package{Provider: "arch"}
			pkgMap[pkgKey] = p
		}

		contentBytes, err := io.ReadAll(io.LimitReader(tr, 10*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s in arch db: %w", header.Name, err)
		}
		content := string(contentBytes)

		switch fileName {
		case "desc":
			parseArchDesc(content, p)
		case "depends":
			parseArchDepends(content, p)
		}
	}

	result := make([]pkg.Package, 0, len(pkgMap))
	for _, p := range pkgMap {
		if p.Name != "" {
			result = append(result, *p)
		}
	}

	return result, nil
}

func parseArchDesc(content string, p *pkg.Package) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") {
			currentSection = strings.Trim(line, "%")
			continue
		}

		switch currentSection {
		case "NAME":
			p.Name = line
		case "VERSION":
			p.Version = line
		case "DESC":
			if p.Description == "" {
				p.Description = line
			} else {
				p.Description += " " + line
			}
		case "URL":
			p.URL = line
		case "SHA256SUM":
			p.Checksum = line
		case "LICENSE":
			if p.License == "" {
				p.License = line
			} else {
				p.License += ", " + line
			}
		case "BUILDDATE":
			if ts, err := strconv.ParseInt(line, 10, 64); err == nil {
				p.BuildDate = ts
			}
		}
	}
}

func parseArchDepends(content string, p *pkg.Package) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") {
			currentSection = strings.Trim(line, "%")
			continue
		}

		if currentSection == "DEPENDS" {
			// e.g. "glibc>=2.38" or "bash"
			depName := line
			depVer := ""
			for _, op := range []string{">=", "<=", "=", ">", "<"} {
				if idx := strings.Index(line, op); idx != -1 {
					depName = line[:idx]
					depVer = line[idx:]
					break
				}
			}
			p.Depends = append(p.Depends, pkg.Dep{
				Name:    depName,
				Version: depVer,
				Type:    "runtime",
			})
		}
	}
}
