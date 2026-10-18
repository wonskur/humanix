package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	pkg "github.com/wonskur/upm/internal/package"
)

type aurRPCResponse struct {
	Version     int            `json:"version"`
	Type        string         `json:"type"`
	ResultCount int            `json:"resultcount"`
	Results     []aurPkgResult `json:"results"`
	Error       string         `json:"error,omitempty"`
}

type aurPkgResult struct {
	Name        string   `json:"Name"`
	Version     string   `json:"Version"`
	Description string   `json:"Description"`
	URL         string   `json:"URL"`
	URLPath     string   `json:"URLPath"`
	License     []string `json:"License"`
	Depends     []string `json:"Depends"`
	MakeDepends []string `json:"MakeDepends"`
}

// AURProvider interacts with the Arch User Repository (AUR) RPC API v5.
type AURProvider struct {
	rpcURL     string
	httpClient *http.Client
}

// NewAURProvider creates a new AUR provider.
func NewAURProvider() *AURProvider {
	return &AURProvider{
		rpcURL:     "https://aur.archlinux.org/rpc/v5",
		httpClient: newSecureClient(15 * time.Second),
	}
}

// SetBaseURL overrides the default RPC URL (used in tests).
func (a *AURProvider) SetBaseURL(u string) {
	a.rpcURL = strings.TrimSuffix(u, "/")
}

// Name returns the provider identifier.
func (a *AURProvider) Name() string {
	return "aur"
}

// Sync is a no-op for AUR since packages are queried dynamically via RPC.
func (a *AURProvider) Sync(ctx context.Context) error {
	return nil
}

// Search searches AUR packages matching the query string.
func (a *AURProvider) Search(ctx context.Context, query string) ([]pkg.Package, error) {
	endpoint := fmt.Sprintf("%s/search/%s", a.rpcURL, url.PathEscape(query))
	return a.queryRPC(ctx, endpoint)
}

// Get retrieves detailed metadata for a specific AUR package.
func (a *AURProvider) Get(ctx context.Context, name string) (*pkg.Package, error) {
	endpoint := fmt.Sprintf("%s/info/%s", a.rpcURL, url.PathEscape(name))
	results, err := a.queryRPC(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("AUR package %q not found", name)
	}
	return &results[0], nil
}

func (a *AURProvider) queryRPC(ctx context.Context, reqURL string) ([]pkg.Package, error) {
	if err := requireHTTPS(reqURL); err != nil && !isLoopbackHTTP(reqURL) {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed creating AUR request: %w", err)
	}
	req.Header.Set("User-Agent", "upm/1.0 (humanix GNU/Linux; universal package manager)")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed contacting AUR RPC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AUR RPC HTTP error %d", resp.StatusCode)
	}

	var data aurRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed decoding AUR RPC response: %w", err)
	}

	if data.Error != "" {
		return nil, fmt.Errorf("AUR RPC error: %s", data.Error)
	}

	var packages []pkg.Package
	for _, res := range data.Results {
		p := pkg.Package{
			Name:        res.Name,
			Version:     res.Version,
			Provider:    "aur",
			Description: res.Description,
			URL:         res.URL,
		}

		if len(res.License) > 0 {
			p.License = strings.Join(res.License, ", ")
		}

		if res.URLPath != "" {
			if strings.HasPrefix(res.URLPath, "http") {
				p.URL = res.URLPath
			} else {
				p.URL = "https://aur.archlinux.org" + res.URLPath
			}
		}

		for _, dep := range res.Depends {
			p.Depends = append(p.Depends, pkg.Dep{
				Name: dep,
				Type: "runtime",
			})
		}
		for _, dep := range res.MakeDepends {
			p.Depends = append(p.Depends, pkg.Dep{
				Name: dep,
				Type: "build",
			})
		}

		packages = append(packages, p)
	}

	return packages, nil
}
