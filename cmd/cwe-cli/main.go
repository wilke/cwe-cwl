// Package main provides the CWL CLI tool entry point.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cwe-cli",
		Short: "CWL Workflow Engine CLI",
		Long:  `Command-line interface for the BV-BRC CWL Workflow Engine`,
	}

	// Global flags
	rootCmd.PersistentFlags().StringP("server", "s", "http://localhost:8080", "CWL service URL")
	rootCmd.PersistentFlags().StringP("token", "t", "", "Authentication token")

	// Add subcommands
	rootCmd.AddCommand(newSubmitCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newCancelCmd())
	rootCmd.AddCommand(newUploadCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newOutputsCmd())
	rootCmd.AddCommand(newStepsCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
