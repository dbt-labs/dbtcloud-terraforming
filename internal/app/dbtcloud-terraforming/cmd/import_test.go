package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dbt-labs/dbtcloud-terraforming/dbtcloud"
	"github.com/go-test/deep"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/samber/lo"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestResourceImport(t *testing.T) {

	changeExpectedJobs := []string{
		"map\\[on_merge\\]",                 // on_merge is optional for now
		"interval_cron",                     // interval_cron is a new option that we don't handle today
		"12 != 1",                           // this is related to interval_cron
		"slice\\[\\d\\]: \\d != <no value>", // this is related to interval_cron
	}

	tests := map[string]struct {
		resourceTypes        string
		listLinkedResources  string
		testdataFilename     string
		changesExpectedRegex []string
		projects             string
	}{

		// account level resource
		"dbt Cloud groups":             {resourceTypes: "dbtcloud_group", testdataFilename: "dbtcloud_group"},
		"dbt Cloud user groups":        {resourceTypes: "dbtcloud_user_groups", testdataFilename: "dbtcloud_user_groups"},
		"dbt Cloud webhooks":           {resourceTypes: "dbtcloud_webhook", testdataFilename: "dbtcloud_webhook"},
		"dbt Cloud notifications":      {resourceTypes: "dbtcloud_notification", testdataFilename: "dbtcloud_notification"},
		"dbt Cloud service tokens":     {resourceTypes: "dbtcloud_service_token", testdataFilename: "dbtcloud_service_token"},
		"dbt Cloud global connections": {resourceTypes: "dbtcloud_global_connection", testdataFilename: "dbtcloud_global_connection"},
		// single resource
		"dbt Cloud BigQuery credentials":   {resourceTypes: "dbtcloud_bigquery_credential", testdataFilename: "dbtcloud_bigquery_credential"},
		"dbt Cloud Databricks credentials": {resourceTypes: "dbtcloud_databricks_credential", testdataFilename: "dbtcloud_databricks_credential", changesExpectedRegex: []string{"---TBD", "databricks"}},
		"dbt Cloud environments":           {resourceTypes: "dbtcloud_environment", testdataFilename: "dbtcloud_environment", changesExpectedRegex: []string{"0 !="}},
		"dbt Cloud jobs":                   {resourceTypes: "dbtcloud_job", testdataFilename: "dbtcloud_job", changesExpectedRegex: changeExpectedJobs},
		"dbt Cloud projects":               {resourceTypes: "dbtcloud_project", testdataFilename: "dbtcloud_project"},
		"dbt Cloud project repository":     {resourceTypes: "dbtcloud_project_repository", testdataFilename: "dbtcloud_project_repository", projects: "43"},
		"dbt Cloud repository":             {resourceTypes: "dbtcloud_repository", testdataFilename: "dbtcloud_repository"},
		"dbt Cloud Snowflake credentials":  {resourceTypes: "dbtcloud_snowflake_credential", testdataFilename: "dbtcloud_snowflake_credential", changesExpectedRegex: []string{"---TBD"}},
		// single resource with filter by project
		"dbt Cloud extended attributes":   {resourceTypes: "dbtcloud_extended_attributes", testdataFilename: "dbtcloud_extended_attributes", projects: "71"},
		"dbt Cloud environment variables": {resourceTypes: "dbtcloud_environment_variable", testdataFilename: "dbtcloud_environment_variable", projects: "71"},
		"dbt Cloud jobs one project":      {resourceTypes: "dbtcloud_job", testdataFilename: "dbtcloud_job_single_project", projects: "43", changesExpectedRegex: []string{"map\\[on_merge\\]"}},
		// multiple at once - linking resources
		"dbt Cloud environments and extended attributes":   {resourceTypes: "dbtcloud_environment,dbtcloud_extended_attributes", testdataFilename: "dbtcloud_env_extended_attributes", listLinkedResources: "dbtcloud_extended_attributes", projects: "71"},
		"dbt Cloud environments and Snowflake credentials": {resourceTypes: "dbtcloud_environment,dbtcloud_snowflake_credential", testdataFilename: "dbtcloud_env_snowflake_credential", listLinkedResources: "dbtcloud_snowflake_credential", projects: "71", changesExpectedRegex: []string{"---TBD", "0 !="}},
		"dbt Cloud projects and envs":                      {resourceTypes: "dbtcloud_project,dbtcloud_environment", testdataFilename: "dbtcloud_project_env", listLinkedResources: "dbtcloud_project"},
		"dbt Cloud webhooks and jobs":                      {resourceTypes: "dbtcloud_webhook,dbtcloud_job", testdataFilename: "dbtcloud_webhook_job", listLinkedResources: "dbtcloud_job", changesExpectedRegex: changeExpectedJobs},
		"dbt Cloud jobs with jobs completion trigger":      {resourceTypes: "dbtcloud_job", testdataFilename: "dbtcloud_job_completion_trigger", listLinkedResources: "dbtcloud_job", projects: "43", changesExpectedRegex: []string{"map\\[on_merge\\]"}}, // this one can fail if we link jobs from other projects
		"dbt Cloud with service tokens and projects":       {resourceTypes: "dbtcloud_service_token,dbtcloud_project", testdataFilename: "dbtcloud_service_token_projects", listLinkedResources: "dbtcloud_project"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Reset the environment variables used in test to ensure we don't
			// have both present at once.

			outputGenerate := ""
			outputImport := ""

			dbtCloudClient = dbtcloud.NewDbtCloudHTTPClient(viper.GetString("host-url"), viper.GetString("token"), viper.GetString("account"), nil)

			projectsParam := []string{}
			if tc.projects != "" {
				projectsParam = []string{"--projects", tc.projects}
			}

			// IMPORTANT!!! we need to reset the lists here otherwise subsequent tests will fail
			resourceTypes = []string{}
			listLinkedResources = []string{}
			listFilterProjects = []int{}
			argsGenerate := []string{"--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", "/Users/bper/dev/dbtcloud-terraforming", "generate", "--resource-types", tc.resourceTypes, "--linked-resource-types", tc.listLinkedResources, "--account", dbtCloudTestAccountID}
			combinedArgsGenerate := append(argsGenerate, projectsParam...)
			outputGenerate, err := executeCommandC(rootCmd, combinedArgsGenerate...)
			if err != nil {
				log.Error(err)
			}

			// IMPORTANT!!! we need to reset the lists here otherwise subsequent tests will fail
			resourceTypes = []string{}
			listLinkedResources = []string{}
			listFilterProjects = []int{}
			argsImport := []string{"--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", "/Users/bper/dev/dbtcloud-terraforming", "import", "--modern-import-block", "--resource-types", tc.resourceTypes, "--linked-resource-types", tc.listLinkedResources, "--account", dbtCloudTestAccountID}
			combinedArgsImport := append(argsImport, projectsParam...)
			outputImport, err = executeCommandC(rootCmd, combinedArgsImport...)
			if err != nil {
				log.Error(err)
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
				log.Errorf("error running NewTerraform: %s", err)
				t.FailNow()
			}

			// Run Terraform Apply
			err = tf.Init(context.Background(), tfexec.Upgrade(true))
			if err != nil {
				log.Errorf("error running Init: %s", err)
				t.FailNow()
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
				filePathGenerate, _ := filepath.Abs(workingDir + "/generate.tf")
				log.Errorf("error running Plan -- %s : %s", filePathGenerate, err)
				t.FailNow()
			}

			planResults, err := tf.ShowPlanFile(context.Background(), absolutePath)
			if err != nil {
				log.Errorf("error showing the plan: %s", err)
				t.FailNow()
			}

			if len(tc.changesExpectedRegex) == 0 {
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
				listFieldsDiffs := []string{}
				for resourceChange := range planResults.ResourceChanges {
					mapResourceBefore := (planResults.ResourceChanges[resourceChange].Change.Before).(map[string]any)
					mapResourceAfter := (planResults.ResourceChanges[resourceChange].Change.After).(map[string]any)
					for k, v := range mapResourceBefore {
						diffs := deep.Equal(v, mapResourceAfter[k])
						if len(diffs) > 0 {
							// listFieldsChanged = append(listFieldsChanged, k)
							listFieldsDiffs = append(listFieldsDiffs, diffs...)
							// t.Log(diffs)
						}
					}
				}

				uniqueFieldsDiffs := lo.Uniq(listFieldsDiffs)
				fieldsChangeFilter := lo.Filter(uniqueFieldsDiffs, func(indivChange string, _ int) bool {
					// for each change, we want to check if it's in the list of expected changes
					for _, changeExpectedRegex := range tc.changesExpectedRegex {
						pattern := changeExpectedRegex

						re, err := regexp.Compile(pattern)
						if err != nil {
							log.Println("Error compiling regex:", err)
							return true
						}

						if re.MatchString(indivChange) {
							return false
						}
					}
					return true
				})

				assert.Emptyf(t, fieldsChangeFilter, "Unexpected changes")
			}
		})
	}
}
