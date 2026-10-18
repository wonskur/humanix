package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	pkg "github.com/wonskur/upm/internal/package"
)

// DB wraps an sql.DB connection with convenience queries.
type DB struct {
	conn *sql.DB
}

// New wraps an existing *sql.DB.
func New(conn *sql.DB) *DB {
	return &DB{conn: conn}
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// Conn returns the raw sql.DB connection.
func (d *DB) Conn() *sql.DB {
	return d.conn
}

// CountAvailable returns the total number of packages in available table.
func (d *DB) CountAvailable(ctx context.Context) (int, error) {
	var count int
	err := d.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM available").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count available packages: %w", err)
	}
	return count, nil
}

// CountInstalled returns the total number of installed packages.
func (d *DB) CountInstalled(ctx context.Context) (int, error) {
	var count int
	err := d.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM installed").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count installed packages: %w", err)
	}
	return count, nil
}

// SaveAvailableBatch writes a list of available packages to the database in a single transaction.
func (d *DB) SaveAvailableBatch(ctx context.Context, pkgs []pkg.Package) error {
	if len(pkgs) == 0 {
		return nil
	}

	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO available (name, version, provider, description, depends, url, checksum)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, p := range pkgs {
		depBytes, err := json.Marshal(p.Depends)
		depStr := ""
		if err == nil {
			depStr = string(depBytes)
		}

		_, err = stmt.ExecContext(ctx,
			p.Name,
			p.Version,
			p.Provider,
			p.Description,
			depStr,
			p.URL,
			p.Checksum,
		)
		if err != nil {
			return fmt.Errorf("failed to insert available package %s (%s): %w", p.Name, p.Provider, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit available packages transaction: %w", err)
	}

	return nil
}

// SearchAvailable queries packages matching name or description.
func (d *DB) SearchAvailable(ctx context.Context, query string) ([]pkg.Package, error) {
	like := "%" + query + "%"
	rows, err := d.conn.QueryContext(ctx, `
		SELECT name, version, provider, description, depends, url, checksum
		FROM available
		WHERE name LIKE ? OR description LIKE ?
		ORDER BY name ASC
	`, like, like)
	if err != nil {
		return nil, fmt.Errorf("failed to search available packages: %w", err)
	}
	defer rows.Close()

	var result []pkg.Package
	for rows.Next() {
		var p pkg.Package
		var depStr string
		if err := rows.Scan(&p.Name, &p.Version, &p.Provider, &p.Description, &depStr, &p.URL, &p.Checksum); err != nil {
			return nil, fmt.Errorf("failed to scan package row: %w", err)
		}
		if depStr != "" {
			_ = json.Unmarshal([]byte(depStr), &p.Depends)
		}
		result = append(result, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading available packages: %w", err)
	}

	return result, nil
}

// GetAvailable retrieves an available package by name and optional provider.
func (d *DB) GetAvailable(ctx context.Context, name, provider string) (*pkg.Package, error) {
	var query string
	var args []any
	if provider != "" {
		query = `SELECT name, version, provider, description, depends, url, checksum FROM available WHERE name = ? AND provider = ? LIMIT 1`
		args = []any{name, provider}
	} else {
		query = `SELECT name, version, provider, description, depends, url, checksum FROM available WHERE name = ? LIMIT 1`
		args = []any{name}
	}

	var p pkg.Package
	var depStr string
	err := d.conn.QueryRowContext(ctx, query, args...).Scan(&p.Name, &p.Version, &p.Provider, &p.Description, &depStr, &p.URL, &p.Checksum)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("package %s not found: %w", name, err)
		}
		return nil, fmt.Errorf("failed to query package %s: %w", name, err)
	}

	if depStr != "" {
		_ = json.Unmarshal([]byte(depStr), &p.Depends)
	}

	return &p, nil
}

// RegisterInstalled stores package metadata, installed files, dependencies, and a history entry in a single transaction.
func (d *DB) RegisterInstalled(ctx context.Context, p *pkg.Package, files []pkg.ExtractedFileInfo) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Insert or replace into installed
	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO installed (name, version, provider, install_date, build_date, license)
		VALUES (?, ?, ?, ?, ?, ?)
	`, p.Name, p.Version, p.Provider, p.InstallDate, p.BuildDate, p.License)
	if err != nil {
		return fmt.Errorf("failed inserting installed package %s: %w", p.Name, err)
	}

	// 2. Remove old files for this package if re-installing
	_, err = tx.ExecContext(ctx, "DELETE FROM files WHERE pkg_name = ?", p.Name)
	if err != nil {
		return fmt.Errorf("failed clearing old files for %s: %w", p.Name, err)
	}

	// 3. Insert files
	if len(files) > 0 {
		fileStmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO files (path, pkg_name, sha256, mode)
			VALUES (?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed preparing files statement: %w", err)
		}
		defer fileStmt.Close()

		for _, f := range files {
			_, err = fileStmt.ExecContext(ctx, f.Path, p.Name, f.SHA256, f.Mode)
			if err != nil {
				return fmt.Errorf("failed inserting file %s: %w", f.Path, err)
			}
		}
	}

	// 4. Update dependencies
	_, err = tx.ExecContext(ctx, "DELETE FROM depends WHERE pkg_name = ?", p.Name)
	if err != nil {
		return fmt.Errorf("failed clearing old dependencies for %s: %w", p.Name, err)
	}

	if len(p.Depends) > 0 {
		depStmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO depends (pkg_name, dep_name, dep_version, dep_type)
			VALUES (?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed preparing depends statement: %w", err)
		}
		defer depStmt.Close()

		for _, dep := range p.Depends {
			_, err = depStmt.ExecContext(ctx, p.Name, dep.Name, dep.Version, dep.Type)
			if err != nil {
				return fmt.Errorf("failed inserting dependency %s: %w", dep.Name, err)
			}
		}
	}

	// 5. Record history
	_, err = tx.ExecContext(ctx, `
		INSERT INTO history (action, pkg_name, old_version, new_version, timestamp)
		VALUES ('install', ?, '', ?, ?)
	`, p.Name, p.Version, p.InstallDate)
	if err != nil {
		return fmt.Errorf("failed inserting history record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed committing install transaction: %w", err)
	}

	return nil
}

// GetInstalled retrieves an installed package by name.
func (d *DB) GetInstalled(ctx context.Context, name string) (*pkg.Package, error) {
	var p pkg.Package
	err := d.conn.QueryRowContext(ctx, `
		SELECT name, version, provider, install_date, build_date, license
		FROM installed
		WHERE name = ?
	`, name).Scan(&p.Name, &p.Version, &p.Provider, &p.InstallDate, &p.BuildDate, &p.License)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("package %s is not installed", name)
		}
		return nil, fmt.Errorf("failed querying installed package %s: %w", name, err)
	}

	// Load dependencies
	rows, err := d.conn.QueryContext(ctx, `
		SELECT dep_name, dep_version, dep_type
		FROM depends
		WHERE pkg_name = ?
	`, name)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var dep pkg.Dep
			var depVer, depType sql.NullString
			if err := rows.Scan(&dep.Name, &depVer, &depType); err == nil {
				if depVer.Valid {
					dep.Version = depVer.String
				}
				if depType.Valid {
					dep.Type = depType.String
				}
				p.Depends = append(p.Depends, dep)
			}
		}
	}

	return &p, nil
}

// ListInstalled returns all installed packages.
func (d *DB) ListInstalled(ctx context.Context) ([]pkg.Package, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT name, version, provider, install_date, build_date, license
		FROM installed
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed querying installed packages: %w", err)
	}
	defer rows.Close()

	var result []pkg.Package
	for rows.Next() {
		var p pkg.Package
		if err := rows.Scan(&p.Name, &p.Version, &p.Provider, &p.InstallDate, &p.BuildDate, &p.License); err != nil {
			return nil, fmt.Errorf("failed scanning installed package: %w", err)
		}
		result = append(result, p)
	}

	return result, nil
}

// GetFiles returns all files associated with an installed package.
func (d *DB) GetFiles(ctx context.Context, pkgName string) ([]pkg.ExtractedFileInfo, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT path, sha256, mode
		FROM files
		WHERE pkg_name = ?
		ORDER BY path ASC
	`, pkgName)
	if err != nil {
		return nil, fmt.Errorf("failed querying files for %s: %w", pkgName, err)
	}
	defer rows.Close()

	var files []pkg.ExtractedFileInfo
	for rows.Next() {
		var f pkg.ExtractedFileInfo
		var sha sql.NullString
		var mode sql.NullInt64
		if err := rows.Scan(&f.Path, &sha, &mode); err != nil {
			return nil, fmt.Errorf("failed scanning file row: %w", err)
		}
		if sha.Valid {
			f.SHA256 = sha.String
		}
		if mode.Valid {
			f.Mode = mode.Int64
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed reading files for %s: %w", pkgName, err)
	}

	return files, nil
}

// FindOwner finds which package owns the specified file path.
func (d *DB) FindOwner(ctx context.Context, filePath string) (string, error) {
	var pkgName string
	err := d.conn.QueryRowContext(ctx, `
		SELECT pkg_name
		FROM files
		WHERE path = ?
		LIMIT 1
	`, filePath).Scan(&pkgName)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("no package owns file %s", filePath)
		}
		return "", fmt.Errorf("failed looking up file owner for %s: %w", filePath, err)
	}
	return pkgName, nil
}

// DeleteInstalled removes an installed package record, its dependencies, files, and logs removal in history.
func (d *DB) DeleteInstalled(ctx context.Context, name string) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var version string
	if err := tx.QueryRowContext(ctx, "SELECT version FROM installed WHERE name = ?", name).Scan(&version); err != nil {
		return fmt.Errorf("failed reading installed package %s before removal: %w", name, err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM installed WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("failed deleting installed package %s: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE pkg_name = ?", name); err != nil {
		return fmt.Errorf("failed deleting files for package %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM depends WHERE pkg_name = ?", name); err != nil {
		return fmt.Errorf("failed deleting dependencies for package %s: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO history (action, pkg_name, old_version, new_version, timestamp)
		VALUES ('remove', ?, ?, '', strftime('%s', 'now'))
	`, name, version); err != nil {
		return fmt.Errorf("failed recording package removal in history: %w", err)
	}

	return tx.Commit()
}

// GetDependents returns names of installed packages that depend on depName.
func (d *DB) GetDependents(ctx context.Context, depName string) ([]string, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT pkg_name FROM depends WHERE dep_name = ?
	`, depName)
	if err != nil {
		return nil, fmt.Errorf("failed querying dependents for %s: %w", depName, err)
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed scanning dependent package: %w", err)
		}
		deps = append(deps, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed reading dependents for %s: %w", depName, err)
	}
	return deps, nil
}

// HistoryEntry represents a transaction record in the history table.
type HistoryEntry struct {
	ID         int64
	Action     string
	PkgName    string
	OldVersion string
	NewVersion string
	Timestamp  int64
}

// GetHistory returns audit history for a package or all packages (if pkgName is empty).
func (d *DB) GetHistory(ctx context.Context, pkgName string) ([]HistoryEntry, error) {
	var query string
	var args []any
	if pkgName != "" {
		query = `SELECT id, action, pkg_name, old_version, new_version, timestamp FROM history WHERE pkg_name = ? ORDER BY id DESC`
		args = []any{pkgName}
	} else {
		query = `SELECT id, action, pkg_name, old_version, new_version, timestamp FROM history ORDER BY id DESC LIMIT 50`
	}

	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed querying history: %w", err)
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var oldVer, newVer sql.NullString
		if err := rows.Scan(&e.ID, &e.Action, &e.PkgName, &oldVer, &newVer, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("failed scanning history entry: %w", err)
		}
		if oldVer.Valid {
			e.OldVersion = oldVer.String
		}
		if newVer.Valid {
			e.NewVersion = newVer.String
		}
		entries = append(entries, e)
	}
	return entries, nil
}
