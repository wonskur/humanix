package repo

import (
	"context"
	"fmt"

	pkg "github.com/wonskur/upm/internal/package"
)

type humanixProvider struct {
	repoURL string
}

func NewhumanixProvider(repoURL string) *humanixProvider {
	return &humanixProvider{repoURL: repoURL}
}

func (w *humanixProvider) Name() string {
	return "humanix"
}

func (w *humanixProvider) Sync(ctx context.Context) error {
	// Native humanix source/binary repo sync
	return fmt.Errorf("humanixProvider.Sync not yet implemented")
}

func (w *humanixProvider) Search(ctx context.Context, query string) ([]pkg.Package, error) {
	return nil, fmt.Errorf("humanixProvider.Search not yet implemented")
}

func (w *humanixProvider) Get(ctx context.Context, name string) (*pkg.Package, error) {
	return nil, fmt.Errorf("humanixProvider.Get not yet implemented")
}
