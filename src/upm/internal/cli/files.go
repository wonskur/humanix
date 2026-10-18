package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/utils"
)

var filesCmd = &cobra.Command{
	Use:   "files <pkg>",
	Short: "List all files installed by a package",
	Long:  "Query the database for all files and directories owned by the specified package.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pkgName := args[0]

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed opening database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)
		files, err := database.GetFiles(cmd.Context(), pkgName)
		if err != nil {
			return fmt.Errorf("failed getting files for %s: %w", pkgName, err)
		}

		if len(files) == 0 {
			if _, err := database.GetInstalled(cmd.Context(), pkgName); err != nil {
				return fmt.Errorf("package %q is not installed", pkgName)
			}
			fmt.Printf("Package %q has no recorded files.\n", pkgName)
			return nil
		}

		for _, f := range files {
			fmt.Println(f.Path)
		}
		return nil
	},
}
