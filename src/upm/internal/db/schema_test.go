package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wonskur/upm/internal/db"
)

func TestSchemaInit(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	defer database.Close()

	d := db.New(database)
	ctx := context.Background()

	availCount, err := d.CountAvailable(ctx)
	if err != nil {
		t.Fatalf("CountAvailable failed: %v", err)
	}
	if availCount != 0 {
		t.Errorf("Expected 0 available packages, got %d", availCount)
	}

	instCount, err := d.CountInstalled(ctx)
	if err != nil {
		t.Fatalf("CountInstalled failed: %v", err)
	}
	if instCount != 0 {
		t.Errorf("Expected 0 installed packages, got %d", instCount)
	}
}
