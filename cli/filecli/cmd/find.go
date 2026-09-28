package cmd

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	findDir string
	findExt string
)

var findCmd = &cobra.Command{
	Use:   "find [patron]",
	Short: "Busca archivos por nombre y/o extensión",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		pattern := ""
		if len(args) == 1 {
			pattern = strings.ToLower(args[0])
		}

		return filepath.WalkDir(findDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if findExt != "" && filepath.Ext(path) != findExt {
				return nil
			}
			if pattern != "" && !strings.Contains(strings.ToLower(d.Name()), pattern) {
				return nil
			}
			fmt.Println(path)
			return nil
		})
	},
}

func init() {
	findCmd.Flags().StringVar(&findDir, "dir", ".", "directorio raíz donde buscar")
	findCmd.Flags().StringVar(&findExt, "ext", "", "filtrar por extensión, ej. .go")
}
