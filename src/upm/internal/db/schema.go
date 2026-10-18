package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS installed (
    name TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    provider TEXT NOT NULL,
    install_date INTEGER,
    build_date INTEGER,
    license TEXT
);

CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    pkg_name TEXT NOT NULL,
    sha256 TEXT,
    mode INTEGER,
    FOREIGN KEY (pkg_name) REFERENCES installed(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS depends (
    pkg_name TEXT NOT NULL,
    dep_name TEXT NOT NULL,
    dep_version TEXT,
    dep_type TEXT,
    PRIMARY KEY (pkg_name, dep_name)
);

CREATE TABLE IF NOT EXISTS available (
    name TEXT,
    version TEXT,
    provider TEXT,
    description TEXT,
    depends TEXT,
    url TEXT,
    checksum TEXT,
    PRIMARY KEY (name, version, provider)
);

CREATE TABLE IF NOT EXISTS history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action TEXT,
    pkg_name TEXT,
    old_version TEXT,
    new_version TEXT,
    timestamp INTEGER
);

CREATE INDEX IF NOT EXISTS idx_files_pkg_name ON files(pkg_name);
CREATE INDEX IF NOT EXISTS idx_depends_pkg_name ON depends(pkg_name);
CREATE INDEX IF NOT EXISTS idx_available_name ON available(name);
`

// Open opens or creates the SQLite database at dbPath and initializes the schema.
func Open(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for database %s: %w", dbPath, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}

	// Enable WAL mode and foreign keys for performance and data integrity
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to execute pragma '%s': %w", pragma, err)
		}
	}

	if _, err := db.Exec(SchemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}
