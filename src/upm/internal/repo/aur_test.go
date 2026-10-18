package repo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAURProvider_SearchAndGet(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/search/spotify" {
			_, _ = fmt.Fprint(w, `{
				"version": 5,
				"type": "search",
				"resultcount": 1,
				"results": [
					{
						"Name": "spotify",
						"Version": "1.2.31-1",
						"Description": "A proprietary music streaming service",
						"URL": "https://www.spotify.com",
						"URLPath": "/cgit/aur.git/snapshot/spotify.tar.gz",
						"License": ["custom"],
						"Depends": ["alsa-lib", "glibc"],
						"MakeDepends": ["git"]
					}
				]
			}`)
			return
		}

		if r.URL.Path == "/info/spotify" {
			_, _ = fmt.Fprint(w, `{
				"version": 5,
				"type": "multiinfo",
				"resultcount": 1,
				"results": [
					{
						"Name": "spotify",
						"Version": "1.2.31-1",
						"Description": "A proprietary music streaming service",
						"URL": "https://www.spotify.com",
						"URLPath": "/cgit/aur.git/snapshot/spotify.tar.gz",
						"License": ["custom"],
						"Depends": ["alsa-lib", "glibc"],
						"MakeDepends": ["git"]
					}
				]
			}`)
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	aur := NewAURProvider()
	aur.SetBaseURL(ts.URL)

	// 1. Test Search
	results, err := aur.Search(context.Background(), "spotify")
	if err != nil {
		t.Fatalf("aur.Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	p := results[0]
	if p.Name != "spotify" {
		t.Errorf("expected name 'spotify', got %q", p.Name)
	}
	if p.Version != "1.2.31-1" {
		t.Errorf("expected version '1.2.31-1', got %q", p.Version)
	}
	if p.Provider != "aur" {
		t.Errorf("expected provider 'aur', got %q", p.Provider)
	}
	if len(p.Depends) != 3 {
		t.Errorf("expected 3 deps, got %d", len(p.Depends))
	}

	// 2. Test Get
	pkgInfo, err := aur.Get(context.Background(), "spotify")
	if err != nil {
		t.Fatalf("aur.Get failed: %v", err)
	}
	if pkgInfo.Name != "spotify" {
		t.Errorf("expected 'spotify', got %q", pkgInfo.Name)
	}

	// 3. Test Not Found
	_, err = aur.Get(context.Background(), "nonexistent-pkg")
	if err == nil {
		t.Fatalf("expected error for nonexistent package, got nil")
	}
}
