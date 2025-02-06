package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(genimportCmd)
}

var genimportCmd = &cobra.Command{
	Use:    "genimport",
	Short:  "Generate Terraform configuration and import commands for dbt Cloud resources",
	Long:   "Combines the functionality of 'generate' and 'import' commands to create Terraform configuration and corresponding import commands in one step",
	Run:    runGenImport(),
	PreRun: sharedPreRun,
}

func runGenImport() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		// First run generate                                                                                         // Start the spinner
		generateResources()(cmd, args)

		// Then run import                                                      // Start the spinner
		runImport()(cmd, args)
	}
}
