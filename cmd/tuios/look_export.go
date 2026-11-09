package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Gaurav-Gosain/tuios/internal/atomicfile"
	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// newLookExportCommand creates `tuios look export`.
func newLookExportCommand() *cobra.Command {
	var out, name, author string
	var all bool
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Print your look as a look file",
		Long: `Print the look in force as a look file. "tuios look apply" takes the file
unchanged.

tuios reads the value of every key that "tuios look keys" lists. The file sets
the keys that differ from the defaults. Use --all to set every key. A theme or
glyph set from your own folder goes into the file. A built-in one goes in by
name.`,
		Example: `  tuios look export
  tuios look export -o my.look.toml --name "Neon 84"
  tuios look export --all`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadLookConfig()
			if err != nil {
				return err
			}
			ex, err := config.ExportLook(cfg, config.LookExportOptions{Name: name, Author: author, All: all})
			if err != nil {
				return err
			}
			for _, n := range ex.Notes {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s.\n", n)
			}
			if out == "" || out == "-" {
				_, err = cmd.OutOrStdout().Write(ex.Text)
				return err
			}
			if err := atomicfile.Write(out, ex.Text, 0o644, false); err != nil {
				return fmt.Errorf("failed to write the look: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tuios wrote the look to %s.\n", out)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "Write the look to this file instead of stdout")
	cmd.Flags().StringVar(&name, "name", "", "The name of the look")
	cmd.Flags().StringVar(&author, "author", "", "The author of the look")
	cmd.Flags().BoolVar(&all, "all", false, "Set every look key, also the ones at their default")
	return cmd
}

// loadLookConfig reads the config in force from disk.
func loadLookConfig() (*config.UserConfig, error) {
	mainPath, err := config.GetConfigPath()
	if err != nil {
		return nil, fmt.Errorf("could not determine config path: %w", err)
	}
	return config.LoadLookConfig(mainPath)
}
