package repo

import (
	"context"
	"strings"
	"testing"
)

func TestRepositorySyncRejectsHTTP(t *testing.T) {
	providers := []struct {
		name string
		sync func(context.Context) error
	}{
		{"arch", NewArchProvider(nil, "http://mirror.example/repo.db").Sync},
		{"debian", NewDebianProvider(nil, "http://mirror.example/Packages.gz").Sync},
		{"fedora", NewFedoraProvider(nil, "http://mirror.example/primary.xml.gz").Sync},
	}
	for _, provider := range providers {
		t.Run(provider.name, func(t *testing.T) {
			err := provider.sync(context.Background())
			if err == nil || !strings.Contains(err.Error(), "HTTPS") {
				t.Fatalf("expected HTTPS rejection, got %v", err)
			}
		})
	}
}
