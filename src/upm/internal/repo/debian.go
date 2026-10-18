package repo

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
)

const (
	DefaultDebianURL = "https://deb.debian.org/debian/dists/trixie/main/binary-amd64/Packages.gz"
)

type DebianProvider struct {
	repoURLs []string
	database *db.DB
	client   *http.Client
}

// NewDebianProvider creates a provider for Debian apt repositories.
func NewDebianProvider(database *db.DB, repoURLs ...string) *DebianProvider {
	if len(repoURLs) == 0 {
		repoURLs = []string{DefaultDebianURL}
	}
	return &DebianProvider{
		repoURLs: repoURLs,
		database: database,
		client:   newSecureClient(120 * time.Second),
	}
}

func (d *DebianProvider) Name() string {
	return "debian"
}

// Sync downloads Packages.gz from Debian mirrors and stores parsed metadata into SQLite.
func (d *DebianProvider) Sync(ctx context.Context) error {
	totalSynced := 0

	for _, rawURL := range d.repoURLs {
		if err := requireHTTPS(rawURL); err != nil {
			return err
		}
		slog.Info("syncing debian repository", "url", rawURL)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request for %s: %w", rawURL, err)
		}

		resp, err := d.client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download debian Packages.gz from %s: %w", rawURL, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("HTTP error %d downloading %s", resp.StatusCode, rawURL)
		}

		limited := &io.LimitedReader{R: resp.Body, N: maxRepositoryDownload + 1}
		pkgs, err := ParseDebianPackages(limited)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse debian Packages file from %s: %w", rawURL, err)
		}
		if limited.N == 0 {
			return fmt.Errorf("repository metadata exceeds maximum size of %d bytes", maxRepositoryDownload)
		}

		if d.database != nil {
			if err := d.database.SaveAvailableBatch(ctx, pkgs); err != nil {
				return fmt.Errorf("failed to save debian packages to database: %w", err)
			}
		}

		slog.Info("debian repository synced", "url", rawURL, "packages", len(pkgs))
		totalSynced += len(pkgs)
	}

	slog.Info("debian provider sync completed", "total_packages", totalSynced)
	return nil
}

// Search searches available Debian packages in database matching query.
func (d *DebianProvider) Search(ctx context.Context, query string) ([]pkg.Package, error) {
	if d.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	all, err := d.database.SearchAvailable(ctx, query)
	if err != nil {
		return nil, err
	}

	var filtered []pkg.Package
	for _, p := range all {
		if p.Provider == "debian" {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// Get finds a package by name in the Debian repository.
func (d *DebianProvider) Get(ctx context.Context, name string) (*pkg.Package, error) {
	if d.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return d.database.GetAvailable(ctx, name, "debian")
}

// ParseDebianPackages parses an RFC822-style Debian Packages.gz stream into a slice of Package structs.
func ParseDebianPackages(r io.Reader) ([]pkg.Package, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader for debian packages: %w", err)
	}
	defer gzr.Close()

	scanner := bufio.NewScanner(gzr)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var packages []pkg.Package
	current := &pkg.Package{Provider: "debian"}
	currentField := ""

	for scanner.Scan() {
		line := scanner.Text()

		// An empty line marks the end of a package entry
		if strings.TrimSpace(line) == "" {
			if current.Name != "" {
				packages = append(packages, *current)
			}
			current = &pkg.Package{Provider: "debian"}
			currentField = ""
			continue
		}

		// Continued multi-line field (starts with space or tab)
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if currentField == "Description" {
				val := strings.TrimSpace(line)
				if val != "." {
					current.Description += " " + val
				}
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		currentField = strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch currentField {
		case "Package":
			current.Name = val
		case "Version":
			current.Version = val
		case "Description":
			current.Description = val
		case "Homepage":
			current.URL = val
		case "SHA256":
			current.Checksum = val
		case "Depends":
			parseDebianDepends(val, current)
		}
	}

	if current.Name != "" {
		packages = append(packages, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed scanning debian packages: %w", err)
	}

	return packages, nil
}

func parseDebianDepends(dependsStr string, p *pkg.Package) {
	// Debian dependencies are comma-separated: "libc6 (>= 2.34), libcurl4 (= 7.88.1)"
	items := strings.Split(dependsStr, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		// Handle alternative dependencies (foo | bar) - pick the first alternative
		if altIdx := strings.Index(item, "|"); altIdx != -1 {
			item = strings.TrimSpace(item[:altIdx])
		}

		depName := item
		depVer := ""
		if openParen := strings.Index(item, "("); openParen != -1 {
			depName = strings.TrimSpace(item[:openParen])
			if closeParen := strings.Index(item, ")"); closeParen != -1 && closeParen > openParen {
				depVer = strings.TrimSpace(item[openParen+1 : closeParen])
			}
		}

		p.Depends = append(p.Depends, pkg.Dep{
			Name:    depName,
			Version: depVer,
			Type:    "runtime",
		})
	}
}
