package resolver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
)

func setupTestDBWithPackages(t *testing.T, pkgs []pkg.Package) *db.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	database := db.New(sqlDB)
	if err := database.SaveAvailableBatch(context.Background(), pkgs); err != nil {
		t.Fatalf("failed saving test available packages: %v", err)
	}

	return database
}

func TestResolver_LinearDependencies(t *testing.T) {
	pkgs := []pkg.Package{
		{
			Name:     "c",
			Version:  "1.0",
			Provider: "humanix",
		},
		{
			Name:     "b",
			Version:  "1.0",
			Provider: "humanix",
			Depends:  []pkg.Dep{{Name: "c"}},
		},
		{
			Name:     "a",
			Version:  "1.0",
			Provider: "humanix",
			Depends:  []pkg.Dep{{Name: "b"}},
		},
	}

	database := setupTestDBWithPackages(t, pkgs)
	defer database.Close()

	r := New(database)
	order, err := r.Resolve(context.Background(), []string{"a"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(order))
	}

	if order[0].Name != "c" || order[1].Name != "b" || order[2].Name != "a" {
		t.Errorf("unexpected dependency order: %v, %v, %v", order[0].Name, order[1].Name, order[2].Name)
	}
}

func TestResolver_DiamondDependencies(t *testing.T) {
	pkgs := []pkg.Package{
		{Name: "base", Version: "1.0", Provider: "humanix"},
		{Name: "lib1", Version: "1.0", Provider: "humanix", Depends: []pkg.Dep{{Name: "base"}}},
		{Name: "lib2", Version: "1.0", Provider: "humanix", Depends: []pkg.Dep{{Name: "base"}}},
		{Name: "app", Version: "1.0", Provider: "humanix", Depends: []pkg.Dep{{Name: "lib1"}, {Name: "lib2"}}},
	}

	database := setupTestDBWithPackages(t, pkgs)
	defer database.Close()

	r := New(database)
	order, err := r.Resolve(context.Background(), []string{"app"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(order) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(order))
	}

	// base must come first, app must come last
	if order[0].Name != "base" {
		t.Errorf("expected base to be first, got %s", order[0].Name)
	}
	if order[3].Name != "app" {
		t.Errorf("expected app to be last, got %s", order[3].Name)
	}
}

func TestResolver_CircularDependency(t *testing.T) {
	pkgs := []pkg.Package{
		{Name: "pkgA", Version: "1.0", Provider: "humanix", Depends: []pkg.Dep{{Name: "pkgB"}}},
		{Name: "pkgB", Version: "1.0", Provider: "humanix", Depends: []pkg.Dep{{Name: "pkgA"}}},
	}

	database := setupTestDBWithPackages(t, pkgs)
	defer database.Close()

	r := New(database)
	_, err := r.Resolve(context.Background(), []string{"pkgA"})
	if err == nil {
		t.Fatalf("expected error on circular dependency, got nil")
	}

	if !strings.Contains(err.Error(), "circular dependency detected") {
		t.Errorf("expected circular dependency error, got %v", err)
	}
}
