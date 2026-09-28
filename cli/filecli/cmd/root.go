package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "filecli",
	Short: "filecli es una herramienta de línea de comandos para explorar archivos",
	Long:  "filecli agrupa utilidades básicas de búsqueda y análisis de archivos, como practica de CLIs en Go con Cobra.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(findCmd)
	rootCmd.AddCommand(wcCmd)
	rootCmd.AddCommand(grepCmd)
}
