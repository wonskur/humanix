package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/repo"
	"github.com/wonskur/upm/internal/utils"
)

var searchFrom string

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search available packages across repositories",
	Long:  "Search for package names and descriptions matching the query string in synchronized repositories or AUR.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		slog.Debug("search query", "query", query, "from", searchFrom)

		if searchFrom == "aur" {
			aur := repo.NewAURProvider()
			pkgs, err := aur.Search(cmd.Context(), query)
			if err != nil {
				return fmt.Errorf("AUR search failed: %w", err)
			}
			if len(pkgs) == 0 {
				fmt.Printf("No AUR packages found matching %q\n", query)
				return nil
			}
			for _, p := range pkgs {
				fmt.Printf("%s/%s %s\n    %s\n", p.Provider, p.Name, p.Version, p.Description)
			}
			return nil
		}

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)
		pkgs, err := database.SearchAvailable(cmd.Context(), query)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}

		var filtered []struct {
			provider string
			name     string
			version  string
			desc     string
		}

		for _, p := range pkgs {
			if searchFrom != "" && p.Provider != searchFrom {
				continue
			}
			filtered = append(filtered, struct {
				provider string
				name     string
				version  string
				desc     string
			}{
				provider: p.Provider,
				name:     p.Name,
				version:  p.Version,
				desc:     p.Description,
			})
		}

		if len(filtered) == 0 {
			fmt.Printf("No packages found matching '%s'\n", query)
			return nil
		}

		for _, p := range filtered {
			fmt.Printf("%s/%s %s\n    %s\n", p.provider, p.name, p.version, p.desc)
		}
		return nil
	},
}

func init() {
	searchCmd.Flags().StringVar(&searchFrom, "from", "", "Filter search by provider (humanix, fedora, debian, arch, aur)")
}
