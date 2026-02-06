// cwe-cli is the command-line interface for the CWL Workflow Engine.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cwe-cli",
		Short: "CWL Workflow Engine CLI",
		Long:  "Command-line interface for submitting and managing CWL workflows on BV-BRC.",
	}

	// Global flags
	rootCmd.PersistentFlags().String("server", getEnvDefault("CWE_SERVER_URL", "http://localhost:8080"), "CWE server URL")
	rootCmd.PersistentFlags().String("token", os.Getenv("CWE_TOKEN"), "Authentication token (or use BVBRC_TOKEN/P3_TOKEN env var)")

	// Add commands
	rootCmd.AddCommand(newSubmitCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newCancelCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newUploadCmd())
	rootCmd.AddCommand(newOutputsCmd())
	rootCmd.AddCommand(newStepsCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getEnvDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
