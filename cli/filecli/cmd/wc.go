package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var wcCmd = &cobra.Command{
	Use:   "wc [archivo]",
	Short: "Cuenta líneas, palabras y caracteres de un archivo",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer f.Close()

		var lines, words, chars int
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			lines++
			chars += len(line) + 1
			words += len(strings.Fields(line))
		}
		if err := scanner.Err(); err != nil {
			return err
		}

		fmt.Printf("%s: %d líneas, %d palabras, %d caracteres\n", args[0], lines, words, chars)
		return nil
	},
}
