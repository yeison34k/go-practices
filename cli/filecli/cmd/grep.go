package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var grepIgnoreCase bool

var grepCmd = &cobra.Command{
	Use:   "grep [patron] [archivo]",
	Short: "Busca líneas que contengan un patrón dentro de un archivo",
	Args:  cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		pattern, path := args[0], args[1]
		if grepIgnoreCase {
			pattern = strings.ToLower(pattern)
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			target := line
			if grepIgnoreCase {
				target = strings.ToLower(line)
			}
			if strings.Contains(target, pattern) {
				fmt.Printf("%d: %s\n", lineNum, line)
			}
		}
		return scanner.Err()
	},
}

func init() {
	grepCmd.Flags().BoolVarP(&grepIgnoreCase, "ignore-case", "i", false, "ignorar mayúsculas/minúsculas")
}
