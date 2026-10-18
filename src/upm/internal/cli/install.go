package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/download"
	"github.com/wonskur/upm/internal/installer"
	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/repo"
	"github.com/wonskur/upm/internal/resolver"
	"github.com/wonskur/upm/internal/utils"
)

var installFrom string

var installCmd = &cobra.Command{
	Use:   "install <pkg>...",
	Short: "Install one or more packages",
	Long:  "Install packages from native repositories or external providers (fedora, debian, arch, aur).",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("install invoked", "packages", args, "from", installFrom, "force", Flags.Force, "dry-run", Flags.DryRun)

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed opening database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)

		var packagesToInstall []pkg.Package

		if Flags.NoDeps {
			for _, name := range args {
				p, err := database.GetAvailable(cmd.Context(), name, installFrom)
				if err != nil {
					return fmt.Errorf("package %q not found in repository: %w", name, err)
				}
				packagesToInstall = append(packagesToInstall, *p)
			}
		} else {
			r := resolver.New(database)
			resolved, err := r.Resolve(cmd.Context(), args)
			if err != nil {
				return fmt.Errorf("failed resolving dependencies: %w", err)
			}
			packagesToInstall = resolved
		}

		if len(packagesToInstall) == 0 {
			fmt.Println("No packages to install.")
			return nil
		}

		fmt.Printf("Packages to install (%d):\n", len(packagesToInstall))
		for _, p := range packagesToInstall {
			fmt.Printf("  - %s (%s, %s)\n", p.Name, p.Version, p.Provider)
		}

		if Flags.DryRun {
			fmt.Printf("\n%sDry-run mode: no changes made to the system.\n", DryRunPrefix())
			return nil
		}

		inst := installer.New(database)
		cacheDir := utils.CacheDir()

		for _, p := range packagesToInstall {
			// Check if already installed
			if !Flags.Force {
				if installed, err := database.GetInstalled(cmd.Context(), p.Name); err == nil && installed != nil {
					fmt.Printf("--> Package %s (%s) is already installed, skipping\n", p.Name, installed.Version)
					continue
				}
			}

			if p.URL == "" {
				// Local or mock install
				if p.Provider == "humanix" || p.Provider == "local" {
					if err := inst.InstallNative(cmd.Context(), &p, ""); err != nil {
						return fmt.Errorf("failed installing native package %s: %w", p.Name, err)
					}
					fmt.Printf("==> Successfully installed native package: %s\n", p.Name)
					continue
				}
				return fmt.Errorf("package %s has no download URL configured", p.Name)
			}
			if p.Provider == "aur" {
				return fmt.Errorf("AUR entry %s is a source repository, not a verified binary archive; source builds must use an isolated build manifest", p.Name)
			}
			if err := download.ValidateSHA256(p.Checksum); err != nil {
				return fmt.Errorf("refusing to install %s without a valid repository SHA256 checksum: %w", p.Name, err)
			}

			candidateURLs := []string{p.URL}
			if p.Provider == "arch" {
				candidateURLs = repo.ArchPackageURLs(p)
			}

			var lastErr error
			var downloaded bool
			var archivePath string
			for _, candidateURL := range candidateURLs {
				archiveName := filepath.Base(candidateURL)
				archivePath = filepath.Join(cacheDir, archiveName)
				if _, err := os.Stat(archivePath); err == nil {
					if err := download.VerifyFileSHA256(archivePath, p.Checksum); err != nil {
						if removeErr := os.Remove(archivePath); removeErr != nil && !os.IsNotExist(removeErr) {
							return fmt.Errorf("failed removing invalid cached archive for %s: %w", p.Name, removeErr)
						}
					} else {
						downloaded = true
						break
					}
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("failed checking package cache for %s: %w", p.Name, err)
				}
				if _, err := os.Stat(archivePath); os.IsNotExist(err) {
					fmt.Printf("--> Downloading %s from %s...\n", p.Name, candidateURL)
					if err := download.DownloadFile(cmd.Context(), candidateURL, archivePath); err != nil {
						lastErr = err
						continue
					}
				}

				if err := download.VerifyFileSHA256(archivePath, p.Checksum); err != nil {
					_ = os.Remove(archivePath)
					lastErr = err
					continue
				}
				downloaded = true
				break
			}
			if !downloaded {
				if lastErr != nil {
					return fmt.Errorf("failed downloading %s: %w", p.Name, lastErr)
				}
				return fmt.Errorf("failed downloading %s: no valid archive URL found", p.Name)
			}

			// Install foreign package
			fmt.Printf("--> Installing foreign package %s into sandbox...\n", p.Name)
			res, err := inst.InstallForeign(cmd.Context(), &p, archivePath)
			if err != nil {
				return fmt.Errorf("failed installing foreign package %s: %w", p.Name, err)
			}

			fmt.Printf("==> Successfully installed %s (%s) [%d files]\n", res.Package.Name, res.Package.Provider, len(res.Files))
		}

		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&installFrom, "from", "", "Specific provider to install from (humanix, fedora, debian, arch, aur)")
}
