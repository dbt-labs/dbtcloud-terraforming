package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dbt-cloud/dbtcloud-terraforming/dbtcloud"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/samber/lo"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestResourceImport(t *testing.T) {
	tests := map[string]struct {
		identifierType      string
		resourceTypes       string
		listLinkedResources string
		testdataFilename    string
		changesExpected     []string
	}{
		// single resource
		"dbt Cloud BigQuery connection":   {identifierType: "account", resourceTypes: "dbtcloud_bigquery_connection", testdataFilename: "dbtcloud_bigquery_connection", changesExpected: []string{"private_key", "application_id", "private_key"}},
		"dbt Cloud BigQuery credentials":  {identifierType: "account", resourceTypes: "dbtcloud_bigquery_credential", testdataFilename: "dbtcloud_bigquery_credential"},
		"dbt Cloud Environments":          {identifierType: "account", resourceTypes: "dbtcloud_environment", testdataFilename: "dbtcloud_environment"},
		"dbt Cloud Environment Variables": {identifierType: "account", resourceTypes: "dbtcloud_environment_variable", testdataFilename: "dbtcloud_environment_variable"},
		"dbt Cloud Groups":                {identifierType: "account", resourceTypes: "dbtcloud_group", testdataFilename: "dbtcloud_group"},
		"dbt Cloud Jobs":                  {identifierType: "account", resourceTypes: "dbtcloud_job", testdataFilename: "dbtcloud_job"},
		"dbt Cloud projects":              {identifierType: "account", resourceTypes: "dbtcloud_project", testdataFilename: "dbtcloud_project"},
		"dbt Cloud project connection":    {identifierType: "account", resourceTypes: "dbtcloud_project_connection", testdataFilename: "dbtcloud_project_connection"},
		"dbt Cloud project repository":    {identifierType: "account", resourceTypes: "dbtcloud_project_repository", testdataFilename: "dbtcloud_project_repository"},
		"dbt Cloud repository":            {identifierType: "account", resourceTypes: "dbtcloud_repository", testdataFilename: "dbtcloud_repository"},
		"dbt Cloud Snowflake credentials": {identifierType: "account", resourceTypes: "dbtcloud_snowflake_credential", testdataFilename: "dbtcloud_snowflake_credential", changesExpected: []string{"password"}},
		// multiple at once
		"dbt Cloud projects and envs": {identifierType: "account", resourceTypes: "dbtcloud_project,dbtcloud_environment", testdataFilename: "dbtcloud_project_env", listLinkedResources: "dbtcloud_project"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Reset the environment variables used in test to ensure we don't
			// have both present at once.

			outputGenerate := ""
			outputImport := ""

			dbtCloudClient = dbtcloud.NewDbtCloudHTTPClient(viper.GetString("host-url"), viper.GetString("token"), viper.GetString("account"), nil)

			// IMPORTANT!!! we need to reset the lists here otherwise subsequent tests will fail
			resourceTypes = []string{}
			listLinkedResources = []string{}
			outputGenerate, err := executeCommandC(rootCmd, "--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", "/Users/bper/dev/dbtcloud-terraforming", "generate", "--resource-types", tc.resourceTypes, "--linked-resource-types", tc.listLinkedResources, "--account", cloudflareTestAccountID)
			if err != nil {
				log.Fatal(err)
			}

			resourceTypes = []string{}
			listLinkedResources = []string{}
			outputImport, err = executeCommandC(rootCmd, "--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", "/Users/bper/dev/dbtcloud-terraforming", "import", "--modern-import-block", "--resource-types", tc.resourceTypes, "--account", cloudflareTestAccountID)
			if err != nil {
				log.Fatal(err)
			}

			workingDir := "../../../../testdata/terraform-import/" + tc.testdataFilename

			err = os.MkdirAll(workingDir, 0755)
			if err != nil {
				log.Fatal(err)
			}

			// we create the providers.tf file if it doesn't exist
			providersTfFile := workingDir + "/provider.tf"
			originalProvidersTfFile := "../../../../testdata/terraform-import/provider.tf"

			// testdata/terraform-import/provider.tf
			if _, err := os.Stat(providersTfFile); os.IsNotExist(err) {
				// File does not exist, proceed to copy

				// Open the source file
				srcFile, err := os.Open(originalProvidersTfFile)
				if err != nil {
					log.Fatalf("Failed to open source file: %s", err)
				}
				defer srcFile.Close()

				// Create the destination file
				dstFile, err := os.Create(providersTfFile)
				if err != nil {
					log.Fatalf("Failed to create destination file: %s", err)
				}
				defer dstFile.Close()

				// Copy the contents of the source file to the destination file
				_, err = io.Copy(dstFile, srcFile)
				if err != nil {
					log.Fatalf("Failed to copy file: %s", err)
				}
			}

			err = os.WriteFile(workingDir+"/generate.tf", []byte(outputGenerate), 0644)
			if err != nil {
				log.Fatal(err)
			}

			err = os.WriteFile(workingDir+"/import.tf", []byte(outputImport), 0644)
			if err != nil {
				log.Fatal(err)
			}

			terraformPath := terraformBinaryPath
			tf, err := tfexec.NewTerraform(workingDir, terraformPath)
			if err != nil {
				log.Fatalf("error running NewTerraform: %s", err)
			}

			// Run Terraform Apply
			err = tf.Init(context.Background(), tfexec.Upgrade(true))
			if err != nil {
				log.Fatalf("error running Init: %s", err)
			}

			// We run terraform plan and save it
			fileString := "../../../../testdata/terraform-import/" + tc.testdataFilename + "/terraform.tfplan"
			file, err := os.Create(fileString)
			if err != nil {
				// Handle the error
				panic(err)
			}
			defer file.Close()

			absolutePath, _ := filepath.Abs(fileString)
			_, err = tf.Plan(context.Background(), tfexec.Out(absolutePath))

			// The following might be better in the future but for now I can't read back the JSON file
			// requireChange, err := tf.PlanJSON(context.Background(), file)
			if err != nil {
				log.Fatalf("error running Init: %s", err)
			}

			planResults, err := tf.ShowPlanFile(context.Background(), absolutePath)
			if err != nil {
				log.Fatalf("error showing the plan: %s", err)
			}

			if len(tc.changesExpected) == 0 {
				// we don't expect changes
				allActions := []string{}
				for resourceChange := range planResults.ResourceChanges {
					for _, action := range planResults.ResourceChanges[resourceChange].Change.Actions {
						allActions = append(allActions, string(action))
					}
				}
				uniqueActions := lo.Uniq(allActions)
				assert.Equal(t, []string{"no-op"}, uniqueActions, "there should be no changes")

			} else {
				// we expect changes but only for specific fields
				listFieldsChanged := []string{}
				for resourceChange := range planResults.ResourceChanges {
					resourceBeforeMap := (planResults.ResourceChanges[resourceChange].Change.Before).(map[string]any)
					resourceAfterMap := (planResults.ResourceChanges[resourceChange].Change.After).(map[string]any)
					for k, v := range resourceBeforeMap {
						if v != resourceAfterMap[k] {
							listFieldsChanged = append(listFieldsChanged, k)
						}
					}
				}

				uniqueFieldsChanged := lo.Uniq(listFieldsChanged)

				assert.Subset(t, tc.changesExpected, uniqueFieldsChanged, "Some fields were not expected to change")
			}
		})
	}
}
