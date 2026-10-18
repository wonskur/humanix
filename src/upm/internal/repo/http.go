package repo

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxRepositoryDownload = 1 << 30

func newSecureClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("refusing repository redirect to non-HTTPS URL")
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many repository redirects")
			}
			return nil
		},
	}
}

func requireHTTPS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return fmt.Errorf("repository URL must use HTTPS: %q", rawURL)
	}
	return nil
}

func isLoopbackHTTP(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	host := parsed.Hostname()
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}
