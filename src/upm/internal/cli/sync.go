package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/repo"
	"github.com/wonskur/upm/internal/utils"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize package databases from configured repositories",
	Long:  "Synchronize local metadata databases with upstream package repositories (humanix, Arch, Debian, Fedora).",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		configPath := utils.ConfigPath()

		configs, err := repo.LoadRepoConfigs(configPath)
		if err != nil {
			return fmt.Errorf("failed to load repository configuration: %w", err)
		}

		if Flags.DryRun {
			slog.Info(DryRunPrefix() + "Dry-run mode: simulating synchronization")
			for _, cfg := range configs {
				if cfg.Enabled {
					fmt.Printf("%sWould sync repository '%s' [%s] from %s\n", DryRunPrefix(), cfg.Name, cfg.Provider, cfg.URL)
				}
			}
			return nil
		}

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)

		syncedRepos := 0
		for _, cfg := range configs {
			if !cfg.Enabled {
				continue
			}

			slog.Info("synchronizing repository", "name", cfg.Name, "provider", cfg.Provider)

			switch cfg.Provider {
			case "arch":
				archProv := repo.NewArchProvider(database, cfg.URL)
				if err := archProv.Sync(ctx); err != nil {
					slog.Error("failed to sync arch repository", "name", cfg.Name, "err", err)
					return fmt.Errorf("failed to sync repo %s: %w", cfg.Name, err)
				}
				syncedRepos++
			case "debian":
				debProv := repo.NewDebianProvider(database, cfg.URL)
				if err := debProv.Sync(ctx); err != nil {
					slog.Error("failed to sync debian repository", "name", cfg.Name, "err", err)
					return fmt.Errorf("failed to sync repo %s: %w", cfg.Name, err)
				}
				syncedRepos++
			case "fedora":
				fedoraProv := repo.NewFedoraProvider(database, cfg.URL)
				if err := fedoraProv.Sync(ctx); err != nil {
					slog.Error("failed to sync fedora repository", "name", cfg.Name, "err", err)
					return fmt.Errorf("failed to sync repo %s: %w", cfg.Name, err)
				}
				syncedRepos++
			case "humanix":
				slog.Info("native humanix provider sync (pending stage 5)")
			default:
				slog.Warn("unknown provider specified in config", "provider", cfg.Provider)
			}
		}

		count, err := database.CountAvailable(ctx)
		if err != nil {
			return fmt.Errorf("failed to count available packages: %w", err)
		}

		fmt.Printf("sync: completed (%d repositories synced, %d packages available)\n", syncedRepos, count)
		return nil
	},
}
