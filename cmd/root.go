package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "scorecard",
	Short: "A CLI tool for various metrics and reporting",
	Long:  "Scorecard is a CLI tool for pulling metrics from various sources and generating reports.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
