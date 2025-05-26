package cmd

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/gosimple/slug"
	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zclconf/go-cty/cty"

	"fmt"
)

var resourceTypes, listLinkedResources []string
var prefixNoQuotes = "~no-quotes~"

func init() {
	rootCmd.AddCommand(generateCmd)
}

var generateCmd = &cobra.Command{
	Use:    "generate",
	Short:  "Fetch resources from the dbt Cloud API and generate the respective Terraform stanzas",
	Run:    generateResources(),
	PreRun: sharedPreRun,
}

type tfVar struct {
	varType        string
	varName        string
	varDescription string
}

var AllTFVars = []tfVar{}

func linkResource(resourceType string) bool {
	if len(listLinkedResources) == 0 {
		return false
	}
	return lo.Contains(listLinkedResources, resourceType) || listLinkedResources[0] == "all"
}

func generateResources() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if outputFile != "" {
			spin := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(cmd.OutOrStderr()))
			spin.Suffix = " Downloading resources and generating config\n"
			spin.Color("purple")
			spin.Start()
			defer spin.Stop()
		}

		if len(resourceTypes) == 0 {
			log.Fatal("you must define at least one --resource-types to generate the config")
		}

		if len(resourceTypes) == 1 && resourceTypes[0] == "all" {
			resourceTypes = lo.Keys(resourceImportStringFormats)
		}

		listFilterProjects = viper.GetIntSlice("projects")

		var execPath, workingDir string
		workingDir = viper.GetString("terraform-install-path")
		execPath = viper.GetString("terraform-binary-path")

		//Download terraform if no existing binary was provided
		if execPath == "" {
			tmpDir, err := os.MkdirTemp("", "tfinstall")
			if err != nil {
				log.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			installConstraints, err := version.NewConstraint("~> 1.0")
			if err != nil {
				log.Fatal("failed to parse version constraints for installation version")
			}

			installer := &releases.LatestVersion{
				Product:     product.Terraform,
				Constraints: installConstraints,
			}

			execPath, err = installer.Install(context.Background())
			if err != nil {
				log.Fatalf("error installing Terraform: %s", err)
			}
		}

		// Setup and configure Terraform to operate in the temporary directory where
		// the provider is already configured.
		log.Debugf("initializing Terraform in %s", workingDir)
		tf, err := tfexec.NewTerraform(workingDir, execPath)
		if err != nil {
			log.Fatal(err)
		}

		log.Debug("reading Terraform schema for dbt Cloud provider")
		ps, err := tf.ProvidersSchema(context.Background())
		if err != nil {
			log.Fatal("failed to read provider schema", err)
		}
		s := ps.Schemas["registry.terraform.io/dbt-labs/dbtcloud"]
		if s == nil {
			log.Fatal("failed to detect provider installation")
		}

		// Create a new empty HCL file for the output
		f := hclwrite.NewEmptyFile()
		rootBody := f.Body()

		// set jobs and projects oustide of the for loop so that we can reference them in other resources
		// and make sure we remove jobs/projects that no longer exist but are still associated with other resources

		// we always get all projects
		prefetchedProjects := dbtCloudClient.GetProjects(listFilterProjects)
		prefetchedProjectsIDs := lo.Map(prefetchedProjects, func(project any, index int) int {
			return int(project.(map[string]any)["id"].(float64))
		})

		// we only get jobs if we need them, there might be a lot of them
		prefetchedJobs := []any{}
		resourceNeedingJobs := []string{"dbtcloud_job", "dbtcloud_webhook", "dbtcloud_notification"}
		if len(lo.Intersect(resourceTypes, resourceNeedingJobs)) > 0 {
			prefetchedJobs = dbtCloudClient.GetJobs(listFilterProjects)
		}
		prefetchedJobsIDs := lo.Map(prefetchedJobs, func(job any, index int) int {
			return int(job.(map[string]any)["id"].(float64))
		})
		// we need this because our API for webhooks returns job IDs as strings
		prefetchedJobsIDsString := lo.Map(prefetchedJobsIDs, func(jobID int, index int) string {
			return fmt.Sprintf("%d", jobID)
		})

		// Process each resource and add to the HCL file
		for _, resourceType := range resourceTypes {
			r := s.ResourceSchemas[resourceType]
			log.Debugf("beginning to read and build %s resources", resourceType)

			// Initialise `resourceCount` outside of the switch for supported resources
			// to allow it to be referenced further down in the loop that outputs the
			// newly generated resources.
			resourceCount := 0

			var jsonStructData []any

			switch resourceType {
			case "dbtcloud_project":

				jsonStructData = prefetchedProjects
				resourceCount = len(jsonStructData)

			case "dbtcloud_job":

				jobs := prefetchedJobs

				for _, job := range jobs {
					jobTyped := job.(map[string]any)

					jobSettings := jobTyped["settings"].(map[string]any)
					jobTyped["num_threads"] = jobSettings["threads"].(float64)
					jobTyped["target_name"] = jobSettings["target_name"].(string)

					jobExecution := jobTyped["execution"].(map[string]any)
					jobTyped["timeout_seconds"] = jobExecution["timeout_seconds"].(float64)

					jobSchedule := jobTyped["schedule"].(map[string]any)
					jobScheduleDate := jobSchedule["date"].(map[string]any)
					jobTyped["schedule_type"] = jobScheduleDate["type"].(string)

					if jobTyped["schedule_type"] == "custom_cron" {
						jobTyped["schedule_cron"] = jobScheduleDate["cron"].(string)
					}
					if jobTyped["schedule_type"] == "interval_cron" {
						jobTyped["schedule_type"] = "custom_cron"
						jobTyped["schedule_cron"] = jobScheduleDate["cron"].(string)
					}
					if jobTyped["schedule_type"] == "days_of_week" {
						jobTyped["schedule_days"] = jobScheduleDate["days"]

						jobScheduleTime := jobSchedule["time"].(map[string]any)
						if jobScheduleTime["type"].(string) == "at_exact_hours" {
							jobTyped["schedule_hours"] = jobScheduleTime["hours"]
						}

						// TODO: Handle the case when this is every x hours
					}

					jobTriggers := jobTyped["triggers"].(map[string]any)

					// we allow deactivating jobs based on a local variable
					var triggers map[string]any
					if parameterizeJobs {
						triggers = map[string]any{
							"github_webhook":       fmt.Sprintf("%slocal.deactivate_jobs_pr ? false : %t", prefixNoQuotes, getBool(jobTriggers["github_webhook"])),
							"git_provider_webhook": fmt.Sprintf("%slocal.deactivate_jobs_pr ? false : %t", prefixNoQuotes, getBool(jobTriggers["git_provider_webhook"])),
							"schedule":             fmt.Sprintf("%slocal.deactivate_jobs_schedule ? false : %t", prefixNoQuotes, getBool(jobTriggers["schedule"])),
							"on_merge":             fmt.Sprintf("%slocal.deactivate_jobs_merge ? false : %t", prefixNoQuotes, getBool(jobTriggers["on_merge"])),
						}
					} else {
						triggers = map[string]any{
							"github_webhook":       getBool(jobTriggers["github_webhook"]),
							"git_provider_webhook": getBool(jobTriggers["git_provider_webhook"]),
							"schedule":             getBool(jobTriggers["schedule"]),
							"on_merge":             getBool(jobTriggers["on_merge"]),
						}
					}

					jobTyped["triggers"] = triggers

					if linkResource("dbtcloud_environment") {
						environmentID := jobTyped["environment_id"].(float64)
						jobTyped["environment_id"] = fmt.Sprintf("%sdbtcloud_environment.terraform_managed_resource_%0.f.environment_id", prefixNoQuotes, environmentID)

						// handle the case when deferring_environment_id is not set
						if deferringEnvironmentID, ok := jobTyped["deferring_environment_id"].(float64); ok {
							jobTyped["deferring_environment_id"] = fmt.Sprintf("%sdbtcloud_environment.terraform_managed_resource_%0.f.environment_id", prefixNoQuotes, deferringEnvironmentID)
						}
					}
					if linkResource("dbtcloud_project") {
						projectID := jobTyped["project_id"].(float64)
						jobTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}

					jobCompletionTrigger, ok := jobTyped["job_completion_trigger_condition"].(map[string]any)
					// if it is not null and actually a map
					if ok {
						jobCompletionTriggerCondition := jobCompletionTrigger["condition"].(map[string]any)

						projectID := jobCompletionTriggerCondition["project_id"].(float64)
						jobID := jobCompletionTriggerCondition["job_id"].(float64)

						completionTriggers := map[string]any{
							"job_id":     jobID,
							"project_id": projectID,
							"statuses":   mapJobStatusCodeToText(jobCompletionTriggerCondition["statuses"].([]any)),
						}

						if linkResource("dbtcloud_job") {
							completionTriggers["job_id"] = fmt.Sprintf("%sdbtcloud_job.terraform_managed_resource_%0.f.id", prefixNoQuotes, jobID)
						}

						if linkResource("dbtcloud_project") {
							completionTriggers["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
						}

						jobTyped["job_completion_trigger_condition"] = completionTriggers
					}

					jsonStructData = append(jsonStructData, jobTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_environment":

				listEnvironments := dbtCloudClient.GetEnvironments(listFilterProjects)

				for _, environment := range listEnvironments {
					environmentsTyped := environment.(map[string]any)
					projectID := environmentsTyped["project_id"].(float64)

					if linkResource("dbtcloud_project") {
						environmentsTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}

					// handle the case when credentialID is not a float because it is null
					if credentialID, ok := environmentsTyped["credentials_id"].(float64); ok {

						environmentsTyped["credential_id"] = credentialID
						if linkResource("dbtcloud_snowflake_credential") || linkResource("dbtcloud_bigquery_credential") || linkResource("dbtcloud_databricks_credential") {

							credentials, credentialsOK := environmentsTyped["credentials"].(map[string]any)

							if credentialsOK {
								credentialsType := credentials["type"].(string)
								adapterVersion := credentials["adapter_version"].(string)

								if lo.Contains([]string{"snowflake", "bigquery"}, credentialsType) {
									environmentsTyped["credential_id"] = fmt.Sprintf("%sdbtcloud_%s_credential.terraform_managed_resource_%0.f.credential_id", prefixNoQuotes, credentialsType, credentialID)
								} else if adapterVersion == "databricks_v0" {
									environmentsTyped["credential_id"] = fmt.Sprintf("%sdbtcloud_databricks_credential.terraform_managed_resource_%0.f.credential_id", prefixNoQuotes, credentialID)
								} else {
									environmentsTyped["credential_id"] = fmt.Sprintf("---TBD---credential type not supported yet for %s---", adapterVersion)
								}

							} else {
								environmentsTyped["credential_id"] = "---TBD---"
							}
						}
					}
					if linkResource("dbtcloud_global_connection") {
						connectionID := environmentsTyped["connection_id"].(float64)
						environmentsTyped["connection_id"] = fmt.Sprintf("%sdbtcloud_global_connection.terraform_managed_resource_%0.f.id", prefixNoQuotes, connectionID)
					}

					// handle the case when extended_attributes_id is not set
					if extendedAttributesID, ok := environmentsTyped["extended_attributes_id"].(float64); ok {
						if linkResource("dbtcloud_extended_attributes") {
							environmentsTyped["extended_attributes_id"] = fmt.Sprintf("%sdbtcloud_extended_attributes.terraform_managed_resource_%0.f.extended_attributes_id", prefixNoQuotes, extendedAttributesID)
						}
					}

					jsonStructData = append(jsonStructData, environmentsTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_repository":

				listRepositories := dbtCloudClient.GetRepositories(listFilterProjects)

				for _, repository := range listRepositories {
					repositoryTyped := repository.(map[string]any)

					projectID := repositoryTyped["project_id"].(float64)
					if linkResource("dbtcloud_project") {
						repositoryTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}
					jsonStructData = append(jsonStructData, repositoryTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_project_repository":

				listProjects := dbtCloudClient.GetProjects(listFilterProjects)

				for _, project := range listProjects {
					projectTyped := project.(map[string]any)
					projectID := projectTyped["id"].(float64)
					projectTyped["project_id"] = projectID
					repositoryID := projectTyped["repository_id"]

					if linkResource("dbtcloud_project") {
						projectID := projectTyped["project_id"]
						projectTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}
					if linkResource("dbtcloud_repository") {
						projectTyped["repository_id"] = fmt.Sprintf("%sdbtcloud_repository.terraform_managed_resource_%0.f.repository_id", prefixNoQuotes, repositoryID)
					}
					if repositoryID != nil {
						jsonStructData = append(jsonStructData, projectTyped)
					}
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_environment_variable":

				mapEnvVars := dbtCloudClient.GetEnvironmentVariables(listFilterProjects)
				listEnvVars := []any{}

				cacheEnvs := []any{}
				// if we want to dynamically link dbtcloud_environment, we need to cache the environments so that we can map them in depends_on
				if linkResource("dbtcloud_environment") {
					cacheEnvs = dbtCloudClient.GetEnvironments(listFilterProjects)
				}

				for projectID, envVars := range mapEnvVars {
					for envVarName, envVarValues := range envVars.(map[string]any) {
						envDetails := map[string]any{}
						envDetails["name"] = envVarName
						envDetails["id"] = fmt.Sprintf("%d_%s", projectID, envVarName)
						envDetails["project_id"] = projectID

						if linkResource("dbtcloud_project") {
							envDetails["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%d.id", prefixNoQuotes, projectID)
						}

						// we need to make int a map[string]any to work with the matching strategy
						collectEnvValues := map[string]any{}

						envVarValuesTyped := envVarValues.(map[string]any)
						listEnvNames := []string{}
						for envName, envValues := range envVarValuesTyped {

							if envName != "project" {
								listEnvNames = append(listEnvNames, envName)
							}

							if envValues != nil {
								envValuesTyped := envValues.(map[string]any)
								collectEnvValues[envName] = envValuesTyped["value"].(string)

								targetURL := fmt.Sprintf("%s/deploy/%s/projects/%d/environments/", dbtCloudClient.HostURL[:len(dbtCloudClient.HostURL)-4], dbtCloudClient.AccountID, projectID)
								if strings.HasPrefix(envVarName, "DBT_ENV_SECRET_") {
									varName := fmt.Sprintf("dbtcloud_environment_variable_%d_%s_%s", projectID, envVarName, slug.Make(envName))
									AllTFVars = append(AllTFVars, tfVar{
										varType:        "string",
										varName:        varName,
										varDescription: "The secret env var for " + envVarName + " in the environment " + envName + " in the project " + fmt.Sprintf("%d", projectID) + " - " + targetURL,
									})
									collectEnvValues[envName] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
								}
							}
						}

						if linkResource("dbtcloud_environment") {
							matchingEnvs := lo.Filter(cacheEnvs, func(i any, index int) bool {
								typedEnv := i.(map[string]any)
								sameProject := typedEnv["project_id"].(float64) == float64(projectID)
								envNameInList := lo.Contains(listEnvNames, (i.(map[string]any)["name"].(string)))
								return sameProject && envNameInList
							})

							listDependsOn := []string{}
							for _, matchingEnv := range matchingEnvs {
								listDependsOn = append(listDependsOn, fmt.Sprintf("%sdbtcloud_environment.terraform_managed_resource_%0.f", prefixNoQuotes, matchingEnv.(map[string]any)["id"].(float64)))
							}
							envDetails["depends_on"] = listDependsOn
						}
						envDetails["environment_values"] = collectEnvValues

						listEnvVars = append(listEnvVars, envDetails)
					}
				}

				jsonStructData = listEnvVars
				resourceCount = len(jsonStructData)

			case "dbtcloud_snowflake_credential":
				listCredentials := dbtCloudClient.GetSnowflakeCredentials(listFilterProjects)

				for _, credential := range listCredentials {
					credentialTyped := credential.(map[string]any)

					projectID := credentialTyped["project_id"].(float64)
					credentialID := credentialTyped["id"].(float64)
					environmentID := credentialTyped["environment_id"].(float64)
					credentialTyped["num_threads"] = credentialTyped["threads"]

					targetURL := fmt.Sprintf("%s/deploy/%s/projects/%0.f/environments/%0.f/settings/", dbtCloudClient.HostURL[:len(dbtCloudClient.HostURL)-4], dbtCloudClient.AccountID, projectID, environmentID)
					switch credentialTyped["auth_type"] {
					case "password":
						varName := fmt.Sprintf("dbtcloud_snowflake_credential_password_%0.f", credentialID)
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The password for the snowflake credential " + fmt.Sprintf("%0.f", credentialID) + " - " + targetURL,
						})
						credentialTyped["password"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
					case "keypair":
						varName := fmt.Sprintf("dbtcloud_snowflake_credential_private_key_%0.f", credentialID)
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The private key for the snowflake credential " + fmt.Sprintf("%0.f", credentialID) + " - " + targetURL,
						})
						credentialTyped["private_key"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
						varNamePassphrase := fmt.Sprintf("dbtcloud_snowflake_credential_private_key_passphrase_%0.f", credentialID)
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varNamePassphrase,
							varDescription: "The passphrase for the snowflake credential " + fmt.Sprintf("%0.f", credentialID) + " - " + targetURL,
						})
						credentialTyped["private_key_passphrase"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varNamePassphrase)
					}

					if linkResource("dbtcloud_project") {
						credentialTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}
					jsonStructData = append(jsonStructData, credentialTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_databricks_credential":
				listCredentials := dbtCloudClient.GetDatabricksCredentials(listFilterProjects)

				for _, credential := range listCredentials {
					credentialTyped := credential.(map[string]any)

					credentialDetails, err := dbtCloudClient.GetCredential(int64(credentialTyped["project_id"].(float64)), int64(credentialTyped["id"].(float64)))
					if err != nil {
						log.Fatal(err)
					}
					for key, value := range credentialDetails.(map[string]any)["unencrypted_credential_details"].(map[string]any) {
						credentialTyped[key] = value
					}

					// we remove the adapter_id as we will use global connections
					credentialTyped["adapter_id"] = ""

					projectID := credentialTyped["project_id"].(float64)
					credentialID := credentialTyped["id"].(float64)
					environmentID := credentialTyped["environment_id"].(float64)
					credentialTyped["adapter_type"] = "databricks"

					targetURL := fmt.Sprintf("%s/deploy/%s/projects/%0.f/environments/%0.f/settings/", dbtCloudClient.HostURL[:len(dbtCloudClient.HostURL)-4], dbtCloudClient.AccountID, projectID, environmentID)
					varName := fmt.Sprintf("dbtcloud_databricks_credential_token_%0.f", credentialID)
					AllTFVars = append(AllTFVars, tfVar{
						varType:        "string",
						varName:        varName,
						varDescription: "The token for the databricks credential " + fmt.Sprintf("%0.f", credentialID) + " - " + targetURL,
					})
					credentialTyped["token"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)

					// the target_name is deprecated at the credentials level
					delete(credentialTyped, "target_name")

					if linkResource("dbtcloud_project") {
						credentialTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}

					jsonStructData = append(jsonStructData, credentialTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_bigquery_credential":
				listCredentials := dbtCloudClient.GetBigQueryCredentials(listFilterProjects)

				for _, credential := range listCredentials {
					credentialTyped := credential.(map[string]any)

					projectID := credentialTyped["project_id"].(float64)
					credentialTyped["num_threads"] = credentialTyped["threads"]
					credentialTyped["dataset"] = credentialTyped["schema"]

					if linkResource("dbtcloud_project") {
						credentialTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}
					jsonStructData = append(jsonStructData, credentialTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_bigquery_connection":
				bigqueryConnections := dbtCloudClient.GetBigQueryConnections(listFilterProjects)
				bigqueryConnectionsTyped := []any{}

				for _, connection := range bigqueryConnections {
					connectionTyped := connection.(map[string]any)
					projectID := connectionTyped["project_id"].(float64)
					connectionID := connectionTyped["id"].(float64)

					connectionDetailsTyped := connectionTyped["details"].(map[string]any)

					// we "promote" all details fields one level up like in the Terraform resource
					for detailKey, detailVal := range connectionDetailsTyped {
						connectionTyped[detailKey] = detailVal
					}
					// we have to put back the ID as it is only set at the top level
					connectionTyped["id"] = connectionID

					// we set the project IDs to the correct values
					// unfortunately project ID can mean a dbt Cloud project or a GCP project
					connectionTyped["project_id"] = projectID
					connectionTyped["gcp_project_id"] = connectionDetailsTyped["project_id"]

					// we add the secure fields
					varName := fmt.Sprintf("dbtcloud_bigquery_connection_private_key_%0.f", connectionID)
					targetURL := fmt.Sprintf("%s/settings/accounts/%s/pages/connections/%0.f/", dbtCloudClient.HostURL[:len(dbtCloudClient.HostURL)-4], dbtCloudClient.AccountID, connectionID)
					AllTFVars = append(AllTFVars, tfVar{
						varType:        "string",
						varName:        varName,
						varDescription: "The private key for the bigquery connection " + fmt.Sprintf("%0.f", connectionID) + " - " + targetURL,
					})
					connectionTyped["private_key"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)

					if linkResource("dbtcloud_project") {
						connectionTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}

					bigqueryConnectionsTyped = append(bigqueryConnectionsTyped, connectionTyped)
				}

				jsonStructData = bigqueryConnectionsTyped
				resourceCount = len(jsonStructData)

			case "dbtcloud_connection":
				genericConnections := dbtCloudClient.GetGenericConnections(listFilterProjects)
				genericConnectionsTyped := []any{}

				for _, connection := range genericConnections {
					connectionTyped := connection.(map[string]any)
					projectID := connectionTyped["project_id"].(float64)
					connectionID := connectionTyped["id"].(float64)
					connectionDetailsTyped := connectionTyped["details"].(map[string]any)

					// we "promote" all details fields one level up like in the Terraform resource
					for detailKey, detailVal := range connectionDetailsTyped {
						connectionTyped[detailKey] = detailVal
					}
					// we have to put back the ID as it is only set at the top level
					connectionTyped["id"] = connectionID

					if connectionTyped["type"] == "snowflake" {
						connectionTyped["oauth_client_id"] = "---TBD if using OAuth, otherwise delete---"
						connectionTyped["oauth_client_secret"] = "---TBD if using OAuth, otherwise delete---"
					}

					if connectionTyped["type"] == "redshift" || connectionTyped["type"] == "postgres" {
						connectionTyped["host_name"] = connectionTyped["hostname"]
						connectionTyped["database"] = connectionTyped["dbname"]
					}

					if connectionTyped["type"] == "adapter" {

						detailsTyped := connectionTyped["details"].(map[string]any)
						connectionDetailsTyped := detailsTyped["connection_details"].(map[string]any)
						fieldsTyped := connectionDetailsTyped["fields"].(map[string]any)
						typeTyped := fieldsTyped["type"].(map[string]any)
						connectionType := fmt.Sprintf("adapter/%s", typeTyped["value"].(string))

						if connectionType == "adapter/databricks" {

							hostnameTyped := fieldsTyped["host"].(map[string]any)
							hostnameVal := hostnameTyped["value"].(string)
							httpPathTyped := fieldsTyped["http_path"].(map[string]any)
							httpPathVal := httpPathTyped["value"].(string)
							catalogTyped := fieldsTyped["catalog"].(map[string]any)
							catalogVal := catalogTyped["value"].(string)

							connectionTyped["host_name"] = hostnameVal
							connectionTyped["http_path"] = httpPathVal
							connectionTyped["catalog"] = catalogVal
							connectionTyped["database"] = "<set-empty-string>"
						}

						// we don't support adapter/spark yet

					}

					if linkResource("dbtcloud_project") {
						connectionTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}

					genericConnectionsTyped = append(genericConnectionsTyped, connectionTyped)
				}

				jsonStructData = genericConnectionsTyped
				resourceCount = len(jsonStructData)

			case "dbtcloud_extended_attributes":
				listExtendedAttributes := dbtCloudClient.GetExtendedAttributes(listFilterProjects)

				for _, extendedAttributes := range listExtendedAttributes {
					extendedAttributesTyped := extendedAttributes.(map[string]any)

					marshalledExtendedAttributes, err := json.Marshal(extendedAttributesTyped["extended_attributes"].(map[string]any))
					if err != nil {
						log.Panicf("Error marshalling extended attributes: %s", err)
					}
					jsonValue := string(marshalledExtendedAttributes)
					extendedAttributesTyped["extended_attributes"] = jsonValue

					projectID := extendedAttributesTyped["project_id"].(float64)
					extendedAttributesTyped["state"] = ""

					if linkResource("dbtcloud_project") {
						extendedAttributesTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, projectID)
					}
					jsonStructData = append(jsonStructData, extendedAttributesTyped)
				}

				resourceCount = len(jsonStructData)

			// not limited by project
			case "dbtcloud_group":

				listGroups := dbtCloudClient.GetGroups()

				for _, group := range listGroups {
					groupTyped := group.(map[string]any)

					defaultGroups := []string{"Owner", "Member", "Everyone"}

					// we check if the group is one of the default ones
					_, ok := lo.Find(defaultGroups, func(i string) bool {
						return i == groupTyped["name"].(string)
					})
					// remove the default groups
					if ok {
						continue
					}

					if linkResource("dbtcloud_project") {

						groupPermissions, ok := groupTyped["group_permissions"].([]any)
						if !ok {
							panic("Could not cast group_permissions to []any")
						}
						newGroupPermissionsTyped := []map[string]any{}
						for _, groupPermission := range groupPermissions {
							groupPermissionTyped := groupPermission.(map[string]any)
							if groupPermissionTyped["all_projects"] == false && lo.Contains(prefetchedProjectsIDs, int(groupPermissionTyped["project_id"].(float64))) {
								groupPermissionTyped["project_id"] = fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%0.f.id", prefixNoQuotes, groupPermissionTyped["project_id"].(float64))
							}
							newGroupPermissionsTyped = append(newGroupPermissionsTyped, groupPermissionTyped)
						}
						groupTyped["group_permissions"] = newGroupPermissionsTyped

					}
					jsonStructData = append(jsonStructData, group)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_user_groups":
				listUsers := dbtCloudClient.GetUsers()

				for _, user := range listUsers {
					userTyped := user.(map[string]any)

					userTyped["user_id"] = userTyped["id"].(float64)

					userPermissionsArray := userTyped["permissions"].([]any)
					userPermissions := userPermissionsArray[0].(map[string]any)
					groupIDs := []int{}

					for _, group := range userPermissions["groups"].([]any) {
						groupTyped := group.(map[string]any)
						groupIDs = append(groupIDs, int(groupTyped["id"].(float64)))
					}
					userTyped["group_ids"] = groupIDs

					if linkResource("dbtcloud_group") {
						linkedGroupIDs := lo.Map(groupIDs, func(i int, index int) string {
							return fmt.Sprintf("%sdbtcloud_group.terraform_managed_resource_%d.id", prefixNoQuotes, i)
						})
						userTyped["group_ids"] = linkedGroupIDs
					}

					jsonStructData = append(jsonStructData, userTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_webhook":

				listWebhooks := dbtCloudClient.GetWebhooks()
				for _, webhook := range listWebhooks {
					webhookTyped := webhook.(map[string]any)

					if linkResource("dbtcloud_job") {
						jobIDs := []string{}
						for _, jobID := range webhookTyped["job_ids"].([]any) {
							jobIDTyped := jobID.(string)
							// we remove jobs that are not relevant to the current project or that have been deleted
							if lo.Contains(prefetchedJobsIDsString, jobIDTyped) {
								jobIDs = append(jobIDs, jobIDTyped)
							}
						}
						linkedJobIDs := lo.Map(jobIDs, func(s string, index int) string {
							return fmt.Sprintf("%sdbtcloud_job.terraform_managed_resource_%s.id", prefixNoQuotes, s)
						})
						webhookTyped["job_ids"] = linkedJobIDs

						// if there is no more job to be linked once filtered, but there are some in the config, we skip the resource
						// because having an empty list of job means "all jobs" from a dbt Cloud API standpoint
						if len(linkedJobIDs) == 0 && len(webhookTyped["job_ids"].([]string)) > 0 {
							continue
						}
					}

					jsonStructData = append(jsonStructData, webhookTyped)
				}
				resourceCount = len(jsonStructData)

			case "dbtcloud_notification":

				listNotifications := dbtCloudClient.GetNotifications()
				for _, notification := range listNotifications {
					notificationTyped := notification.(map[string]any)

					notificationTyped["notification_type"] = notificationTyped["type"]
					notificationTyped["state"] = nil

					if notificationTyped["notification_type"].(float64) == 4 && notificationTyped["external_email"] == nil {
						// for some reason there are external notifications without an email
						continue
					}

					if linkResource("dbtcloud_job") {
						listOns := []string{"on_cancel", "on_failure", "on_success", "on_warning"}

						for _, notifHook := range listOns {

							jobIDs := []float64{}

							for _, jobID := range notificationTyped[notifHook].([]any) {
								jobIDTyped := jobID.(float64)
								// we remove jobs that are not relevant to the current project or that have been deleted
								if lo.Contains(prefetchedJobsIDs, int(jobIDTyped)) {
									jobIDs = append(jobIDs, jobIDTyped)
								}
							}
							linkedJobIDs := lo.Map(jobIDs, func(f float64, index int) string {
								return fmt.Sprintf("%sdbtcloud_job.terraform_managed_resource_%.0f.id", prefixNoQuotes, f)
							})
							notificationTyped[notifHook] = linkedJobIDs
						}
					}

					jsonStructData = append(jsonStructData, notificationTyped)
				}
				resourceCount = len(jsonStructData)

			case "dbtcloud_service_token":

				listServiceTokens := dbtCloudClient.GetServiceTokens()
				for _, serviceToken := range listServiceTokens {

					serviceTokenTyped := serviceToken.(map[string]any)
					serviceTokenTyped["uid"] = nil
					serviceTokenID := int(serviceTokenTyped["id"].(float64))

					permissions := dbtCloudClient.GetServiceTokenPermissions(serviceTokenID)

					if linkResource("dbtcloud_project") {
						permissionsFilteredProjects := []any{}
						for _, permissionsSet := range permissions {
							permissionsSetTyped := permissionsSet.(map[string]any)
							if permissionsSetTyped["project_id"] != nil && lo.Contains(prefetchedProjectsIDs, int(permissionsSetTyped["project_id"].(float64))) {
								projectID := permissionsSetTyped["project_id"].(float64)
								projectResources := fmt.Sprintf("%sdbtcloud_project.terraform_managed_resource_%.0f.id", prefixNoQuotes, projectID)
								permissionsSetTyped["project_id"] = projectResources
								permissionsFilteredProjects = append(permissionsFilteredProjects, permissionsSetTyped)
							}
						}
						permissions = permissionsFilteredProjects
					}

					serviceTokenTyped["service_token_permissions"] = permissions

					jsonStructData = append(jsonStructData, serviceTokenTyped)
				}
				resourceCount = len(jsonStructData)

			case "dbtcloud_global_connection":

				listConnections := dbtCloudClient.GetGlobalConnections()

				for _, connection := range listConnections {
					connectionTyped := connection.(map[string]any)

					configSection := getAdapterFromAdapterVersion(connectionTyped["adapter_version"].(string))

					configTyped := connectionTyped["config"].(map[string]any)
					delete(configTyped, "adapter_id")
					targetURL := fmt.Sprintf("%s/settings/accounts/%s/pages/connections/%0.f/", dbtCloudClient.HostURL[:len(dbtCloudClient.HostURL)-4], dbtCloudClient.AccountID, connectionTyped["id"].(float64))

					// handle the fields that don't come back from the API
					if _, exists := configTyped["oauth_client_id"]; exists {
						varName := fmt.Sprintf("dbtcloud_global_connection_oauth_client_id_%0.f", connectionTyped["id"].(float64))
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The OAuth client ID for the global connection " + fmt.Sprintf("%0.f", connectionTyped["id"].(float64)) + " - " + targetURL,
						})
						configTyped["oauth_client_id"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
					}
					if _, exists := configTyped["oauth_client_secret"]; exists {
						varName := fmt.Sprintf("dbtcloud_global_connection_oauth_client_secret_%0.f", connectionTyped["id"].(float64))
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The OAuth client secret for the global connection " + fmt.Sprintf("%0.f", connectionTyped["id"].(float64)) + " - " + targetURL,
						})
						configTyped["oauth_client_secret"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
					}
					if _, exists := configTyped["private_key"]; exists {
						varName := fmt.Sprintf("dbtcloud_global_connection_private_key_%0.f", connectionTyped["id"].(float64))
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The private key for the global connection " + fmt.Sprintf("%0.f", connectionTyped["id"].(float64)) + " - " + targetURL,
						})
						configTyped["private_key"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
					}
					if _, exists := configTyped["application_id"]; exists {
						varName := fmt.Sprintf("dbtcloud_global_connection_application_id_%0.f", connectionTyped["id"].(float64))
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The application ID for the global connection " + fmt.Sprintf("%0.f", connectionTyped["id"].(float64)) + " - " + targetURL,
						})
						configTyped["application_id"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
					}
					if _, exists := configTyped["application_secret"]; exists {
						varName := fmt.Sprintf("dbtcloud_global_connection_application_secret_%0.f", connectionTyped["id"].(float64))
						AllTFVars = append(AllTFVars, tfVar{
							varType:        "string",
							varName:        varName,
							varDescription: "The application secret for the global connection " + fmt.Sprintf("%0.f", connectionTyped["id"].(float64)) + " - " + targetURL,
						})
						configTyped["application_secret"] = fmt.Sprintf("%svar.%s", prefixNoQuotes, varName)
					}
					// For BQ, to handle the renaming of the fields
					if gcpProjectID, exists := configTyped["project_id"]; exists && configSection == "bigquery" {
						configTyped["gcp_project_id"] = gcpProjectID
						delete(configTyped, "project_id")
					}

					connectionTyped[configSection] = configTyped
					jsonStructData = append(jsonStructData, connectionTyped)

				}

				resourceCount = len(jsonStructData)

			default:
				fmt.Fprintf(cmd.OutOrStderr(), "%q is not yet supported for automatic generation", resourceType)
				return
			}

			// If we don't have any resources to generate, just bail out early.
			if resourceCount == 0 {
				fmt.Fprintf(cmd.OutOrStderr(), "# no resources of type %q found to generate\n", resourceType)
				continue
			}

			for i := 0; i < resourceCount; i++ {
				structData := jsonStructData[i].(map[string]interface{})

				resourceID := ""
				if os.Getenv("USE_STATIC_RESOURCE_IDS") == "true" {
					resourceID = "terraform_managed_resource"
				} else {
					id := ""
					switch structData["id"].(type) {
					case float64:
						id = fmt.Sprintf("%.0f", structData["id"].(float64))
					case nil:
						panic(fmt.Sprintf("There is no `id` defined for the resource %s", resourceType))
					default:
						id = structData["id"].(string)
					}

					resourceID = fmt.Sprintf("terraform_managed_resource_%s", id)
				}
				resource := rootBody.AppendNewBlock("resource", []string{resourceType, resourceID}).Body()

				sortedBlockAttributes := make([]string, 0, len(r.Block.Attributes))
				for k := range r.Block.Attributes {
					sortedBlockAttributes = append(sortedBlockAttributes, k)
				}
				sort.Strings(sortedBlockAttributes)

				// Block attributes are for any attributes where assignment is involved.
				for _, attrName := range sortedBlockAttributes {
					log.Debugf("checking the attribute %s", attrName)
					// Don't bother outputting the ID for the resource as that is only for
					// internal use (such as importing state).
					if attrName == "id" {
						continue
					}

					// No need to output computed attributes that are also not
					// optional.
					if r.Block.Attributes[attrName].Computed && !r.Block.Attributes[attrName].Optional {
						continue
					}
					if attrName == "account_id" && accountID != "" {
						writeAttrLine(attrName, accountID, "", resource)
						continue
					}

					// This is to handle Attributes in the Framework
					if r.Block.Attributes[attrName].AttributeType == cty.NilType {
						writeAttrLine(attrName, structData[attrName], "", resource)
						continue
					}

					ty := r.Block.Attributes[attrName].AttributeType
					switch {
					case ty.IsPrimitiveType():
						switch ty {
						case cty.String, cty.Bool, cty.Number:
							writeAttrLine(attrName, structData[attrName], "", resource)
							delete(structData, attrName)
						default:
							log.Debugf("unexpected primitive type %q", ty.FriendlyName())
						}
					case ty.IsCollectionType():
						switch {
						case ty.IsListType(), ty.IsSetType(), ty.IsMapType():
							writeAttrLine(attrName, structData[attrName], "", resource)
							delete(structData, attrName)
						default:
							log.Debugf("unexpected collection type %q", ty.FriendlyName())
						}
					case ty.IsTupleType():
						fmt.Printf("tuple found. attrName %s\n", attrName)
					case ty.IsObjectType():
						fmt.Printf("object found. attrName %s\n", attrName)
					default:
						log.Debugf("attribute %q (attribute type of %q) has not been generated", attrName, ty.FriendlyName())
					}
				}

				processBlocks(r.Block, jsonStructData[i].(map[string]interface{}), resource, "")
				rootBody.AppendNewline()
			}
		}

		// Add the variables
		if len(AllTFVars) > 0 {
			// Add a comment to the file
			comment := hclwrite.Tokens{
				&hclwrite.Token{
					Type:         hclsyntax.TokenComment,
					Bytes:        []byte("# The variables defined for fields we couldn't retrieve\n\n"),
					SpacesBefore: 0,
				},
			}
			rootBody.AppendUnstructuredTokens(comment)

			for _, tfVar := range AllTFVars {
				variablesBlock := rootBody.AppendNewBlock("variable", []string{tfVar.varName}).Body()
				hclTokens := []*hclwrite.Token{{Type: hclsyntax.TokenIdent, Bytes: []byte(tfVar.varType)}}
				variablesBlock.SetAttributeRaw("type", hclTokens)
				variablesBlock.SetAttributeValue("description", cty.StringVal(tfVar.varDescription))
				rootBody.AppendNewline()
			}
		}

		// Add locals block if parameterizeJobs is true
		if parameterizeJobs {

			comment := hclwrite.Tokens{
				&hclwrite.Token{
					Type:         hclsyntax.TokenComment,
					Bytes:        []byte("# The locals used to activate/deactivate jobs\n\n"),
					SpacesBefore: 0,
				},
			}
			rootBody.AppendUnstructuredTokens(comment)
			localsBlock := rootBody.AppendNewBlock("locals", nil).Body()
			localsBlock.SetAttributeValue("deactivate_jobs_pr", cty.BoolVal(false))
			localsBlock.SetAttributeValue("deactivate_jobs_schedule", cty.BoolVal(false))
			localsBlock.SetAttributeValue("deactivate_jobs_merge", cty.BoolVal(false))
			rootBody.AppendNewline()
		}

		// Add template for the variable values
		if len(AllTFVars) > 0 {
			// Add a comment to the file
			comment := hclwrite.Tokens{
				&hclwrite.Token{
					Type:         hclsyntax.TokenComment,
					Bytes:        []byte("# Copy past the following lines in terraform.tfvars\n\n"),
					SpacesBefore: 0,
				},
			}
			rootBody.AppendUnstructuredTokens(comment)

			for _, tfVar := range AllTFVars {
				comment := hclwrite.Tokens{
					&hclwrite.Token{
						Type:         hclsyntax.TokenComment,
						Bytes:        []byte(fmt.Sprintf("# %s = \"\"", tfVar.varName)),
						SpacesBefore: 0,
					},
				}
				rootBody.AppendUnstructuredTokens(comment)
				rootBody.AppendNewline()
			}
			rootBody.AppendNewline()
			rootBody.AppendNewline()
		}

		// Format the output
		output := string(hclwrite.Format(f.Bytes()))

		// Write the formatted output
		if err := writeString(output); err != nil {
			log.Fatalf("failed to write output: %v", err)
		}
	}
}
