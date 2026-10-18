package repo

import (
	"context"

	pkg "github.com/wonskur/upm/internal/package"
)

// Provider defines the interface for repository sources (humanix, fedora, debian, arch, aur).
type Provider interface {
	Name() string
	Sync(ctx context.Context) error
	Search(ctx context.Context, query string) ([]pkg.Package, error)
	Get(ctx context.Context, name string) (*pkg.Package, error)
}
