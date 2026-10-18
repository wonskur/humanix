package repo

import (
	"compress/gzip"
	"context"
	"encoding/xml"
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
	DefaultFedoraURL = "https://download.fedoraproject.org/pub/fedora/linux/releases/39/Everything/x86_64/os/repodata/primary.xml.gz"
)

type FedoraProvider struct {
	repoURLs []string
	database *db.DB
	client   *http.Client
}

// NewFedoraProvider creates a provider for Fedora RPM repositories.
func NewFedoraProvider(database *db.DB, repoURLs ...string) *FedoraProvider {
	if len(repoURLs) == 0 {
		repoURLs = []string{DefaultFedoraURL}
	}
	return &FedoraProvider{
		repoURLs: repoURLs,
		database: database,
		client:   newSecureClient(180 * time.Second),
	}
}

func (f *FedoraProvider) Name() string {
	return "fedora"
}

// Sync downloads primary.xml.gz from Fedora mirrors and stores parsed metadata into SQLite.
func (f *FedoraProvider) Sync(ctx context.Context) error {
	totalSynced := 0

	for _, rawURL := range f.repoURLs {
		if err := requireHTTPS(rawURL); err != nil {
			return err
		}
		slog.Info("syncing fedora repository", "url", rawURL)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request for %s: %w", rawURL, err)
		}

		resp, err := f.client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download fedora primary.xml.gz from %s: %w", rawURL, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("HTTP error %d downloading %s", resp.StatusCode, rawURL)
		}

		limited := &io.LimitedReader{R: resp.Body, N: maxRepositoryDownload + 1}
		pkgs, err := ParseFedoraPrimaryXML(limited)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse fedora primary.xml from %s: %w", rawURL, err)
		}
		if limited.N == 0 {
			return fmt.Errorf("repository metadata exceeds maximum size of %d bytes", maxRepositoryDownload)
		}

		if f.database != nil {
			if err := f.database.SaveAvailableBatch(ctx, pkgs); err != nil {
				return fmt.Errorf("failed to save fedora packages to database: %w", err)
			}
		}

		slog.Info("fedora repository synced", "url", rawURL, "packages", len(pkgs))
		totalSynced += len(pkgs)
	}

	slog.Info("fedora provider sync completed", "total_packages", totalSynced)
	return nil
}

// Search searches available Fedora packages in database matching query.
func (f *FedoraProvider) Search(ctx context.Context, query string) ([]pkg.Package, error) {
	if f.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	all, err := f.database.SearchAvailable(ctx, query)
	if err != nil {
		return nil, err
	}

	var filtered []pkg.Package
	for _, p := range all {
		if p.Provider == "fedora" {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// Get finds a package by name in the Fedora repository.
func (f *FedoraProvider) Get(ctx context.Context, name string) (*pkg.Package, error) {
	if f.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return f.database.GetAvailable(ctx, name, "fedora")
}

type rpmPackageXML struct {
	Name    string `xml:"name"`
	Arch    string `xml:"arch"`
	Version struct {
		Ver string `xml:"ver,attr"`
		Rel string `xml:"rel,attr"`
	} `xml:"version"`
	Checksum struct {
		Value string `xml:",chardata"`
		Type  string `xml:"type,attr"`
	} `xml:"checksum"`
	Summary     string `xml:"summary"`
	Description string `xml:"description"`
	URL         string `xml:"url"`
	Format      struct {
		License  string `xml:"license"`
		Requires struct {
			Entries []struct {
				Name  string `xml:"name,attr"`
				Flags string `xml:"flags,attr"`
				Ver   string `xml:"ver,attr"`
			} `xml:"entry"`
		} `xml:"requires"`
	} `xml:"format"`
}

// ParseFedoraPrimaryXML streams and parses primary.xml.gz into a slice of Package structs.
func ParseFedoraPrimaryXML(r io.Reader) ([]pkg.Package, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader for fedora primary.xml: %w", err)
	}
	defer gzr.Close()

	decoder := xml.NewDecoder(gzr)
	var packages []pkg.Package

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed decoding xml token: %w", err)
		}

		startElem, ok := token.(xml.StartElement)
		if !ok || startElem.Name.Local != "package" {
			continue
		}

		var xmlPkg rpmPackageXML
		if err := decoder.DecodeElement(&xmlPkg, &startElem); err != nil {
			return nil, fmt.Errorf("failed decoding package element: %w", err)
		}

		ver := xmlPkg.Version.Ver
		if xmlPkg.Version.Rel != "" {
			ver += "-" + xmlPkg.Version.Rel
		}

		p := pkg.Package{
			Name:        xmlPkg.Name,
			Version:     ver,
			Provider:    "fedora",
			Description: strings.TrimSpace(xmlPkg.Summary),
			URL:         xmlPkg.URL,
			License:     xmlPkg.Format.License,
		}
		if strings.EqualFold(xmlPkg.Checksum.Type, "sha256") {
			p.Checksum = strings.TrimSpace(xmlPkg.Checksum.Value)
		}

		for _, req := range xmlPkg.Format.Requires.Entries {
			// Skip internal rpmlib dependencies (e.g. rpmlib(PayloadFilesHavePrefix))
			if strings.HasPrefix(req.Name, "rpmlib(") {
				continue
			}
			p.Depends = append(p.Depends, pkg.Dep{
				Name:    req.Name,
				Version: req.Ver,
				Type:    "runtime",
			})
		}

		packages = append(packages, p)
	}

	return packages, nil
}
