package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(genimportCmd)
}

var genimportCmd = &cobra.Command{
	Use:    "genimport",
	Short:  "Generate Terraform resources configuration and import commands for dbt Cloud resources",
	Long:   "Combines the functionality of 'generate' and 'import' commands to create Terraform configuration and corresponding import commands in one step",
	Run:    runGenImport(),
	PreRun: sharedPreRun,
}

func runGenImport() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		// Temporarily redirect output file for generate step
		originalOutputFile := outputFile
		if outputFile != "" {
			// Use a temporary file for generate output
			outputFile = outputFile + fmt.Sprintf(".%d.temp", time.Now().UnixNano())
		}

		// Run generate
		generateResources()(cmd, args)

		// Store generate output if writing to file
		var generateOutput string
		if originalOutputFile != "" {
			data, err := os.ReadFile(outputFile)
			if err != nil {
				log.Fatal(err)
			}
			generateOutput = string(data)
			// Clean up temp file
			os.Remove(outputFile)
			// Restore original output file path
			outputFile = originalOutputFile
		}

		// Run import
		runImport()(cmd, args)

		// If we're writing to a file, append generate output before import output
		if outputFile != "" {
			data, err := os.ReadFile(outputFile)
			if err != nil {
				log.Fatal(err)
			}
			importOutput := string(data)

			// Combine outputs and write to file
			combinedOutput := generateOutput + "\n" + importOutput
			err = os.WriteFile(outputFile, []byte(combinedOutput), 0644)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}
