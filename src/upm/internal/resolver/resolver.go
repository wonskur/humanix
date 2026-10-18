package resolver

import (
	"context"
	"fmt"

	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
)

// Resolver calculates dependency order using topological sorting and detects cycles.
type Resolver struct {
	database *db.DB
}

// New creates a new Resolver.
func New(database *db.DB) *Resolver {
	return &Resolver{
		database: database,
	}
}

// Resolve returns an ordered list of packages to install given root package names.
// Dependencies are ordered before the packages that depend on them.
func (r *Resolver) Resolve(ctx context.Context, roots []string) ([]pkg.Package, error) {
	if len(roots) == 0 {
		return nil, nil
	}

	pkgMap := make(map[string]pkg.Package)
	adjList := make(map[string][]string)

	// Breadth-first or depth-first search to discover all transitive dependencies
	queue := append([]string{}, roots...)
	visitedQueue := make(map[string]bool)

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visitedQueue[curr] {
			continue
		}
		visitedQueue[curr] = true

		var p *pkg.Package
		var err error

		if r.database != nil {
			p, err = r.database.GetAvailable(ctx, curr, "")
		}

		if err != nil || p == nil {
			return nil, fmt.Errorf("package %q not found in repository: %w", curr, err)
		}

		pkgMap[curr] = *p

		for _, dep := range p.Depends {
			// Skip optional or build dependencies in runtime resolver
			if dep.Type != "" && dep.Type != "runtime" {
				continue
			}
			depName := dep.Name
			adjList[curr] = append(adjList[curr], depName)

			if !visitedQueue[depName] {
				queue = append(queue, depName)
			}
		}
	}

	// Topological sort using DFS with cycle detection
	// 0: unvisited, 1: visiting, 2: visited
	state := make(map[string]int)
	var ordered []pkg.Package

	var visit func(node string, path []string) error
	visit = func(node string, path []string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch state[node] {
		case 1:
			return fmt.Errorf("circular dependency detected: %v -> %s", append(path, node), node)
		case 2:
			return nil
		}

		state[node] = 1 // visiting
		path = append(path, node)

		for _, dep := range adjList[node] {
			if err := visit(dep, path); err != nil {
				return err
			}
		}

		state[node] = 2 // visited
		if p, ok := pkgMap[node]; ok {
			ordered = append(ordered, p)
		}
		return nil
	}

	for _, root := range roots {
		if state[root] == 0 {
			if err := visit(root, nil); err != nil {
				return nil, err
			}
		}
	}

	return ordered, nil
}
