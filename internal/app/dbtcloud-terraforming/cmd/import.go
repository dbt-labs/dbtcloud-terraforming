package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zclconf/go-cty/cty"
)

// resourceImportStringFormats contains a mapping of the resource type to the
// composite ID that is compatible with performing an import.
var resourceImportStringFormats = map[string]string{
	"dbtcloud_project":               ":id",
	"dbtcloud_repository":            ":project_id::id",
	"dbtcloud_project_repository":    ":id::repository_id",
	"dbtcloud_job":                   ":id",
	"dbtcloud_environment":           ":project_id::id",
	"dbtcloud_environment_variable":  ":project_id::name",
	"dbtcloud_group":                 ":id",
	"dbtcloud_snowflake_credential":  ":project_id::id",
	"dbtcloud_databricks_credential": ":project_id::id",
	"dbtcloud_bigquery_credential":   ":project_id::id",
	"dbtcloud_bigquery_connection":   ":project_id::id",
	"dbtcloud_connection":            ":project_id::id",
	"dbtcloud_extended_attributes":   ":project_id::id",
	"dbtcloud_user_groups":           ":user_id",
	"dbtcloud_webhook":               ":id",
	"dbtcloud_notification":          ":id",
	"dbtcloud_service_token":         ":id",
	"dbtcloud_global_connection":     ":id",
}

func init() {
	rootCmd.AddCommand(importCommand)
}

var importCommand = &cobra.Command{
	Use:    "import",
	Short:  "Output `terraform import` compatible commands and/or import blocks (require terraform >= 1.5) in order to import resources into state",
	Run:    runImport(),
	PreRun: sharedPreRun,
}

func runImport() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if outputFile != "" {
			spin := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(cmd.OutOrStderr()))
			spin.Suffix = " Downloading resources and generating import statements\n"
			spin.Start()
			defer spin.Stop()
		}

		if len(resourceTypes) == 0 {
			log.Fatal("you must define at least one --resource-types to generate the import commands/code")
		}
		var jsonStructData []interface{}

		accountID = viper.GetString("account")
		apiToken = viper.GetString("token")
		hostURL = viper.GetString("host-url")
		if hostURL == "" {
			hostURL = "https://cloud.getdbt.com/api"
		}

		if len(resourceTypes) == 1 && resourceTypes[0] == "all" {
			resourceTypes = lo.Keys(resourceImportStringFormats)
		}

		if len(excludeResourceTypes) > 0 {
			resourceTypes = lo.Filter(resourceTypes, func(resourceType string, _ int) bool {
				return !lo.Contains(excludeResourceTypes, resourceType)
			})
		}

		importFile := hclwrite.NewEmptyFile()
		importBody := importFile.Body()

		prefetchedJobs := []any{}
		resourceNeedingJobs := []string{"dbtcloud_job", "dbtcloud_webhook"}
		if len(lo.Intersect(resourceTypes, resourceNeedingJobs)) > 0 {
			prefetchedJobs = dbtCloudClient.GetJobs(listFilterProjects)
		}
		prefetchedJobsIDsAny := lo.Map(prefetchedJobs, func(job any, index int) any {
			return job.(map[string]any)["id"]
		})

		for _, resourceType := range resourceTypes {

			switch resourceType {

			case "dbtcloud_project":
				jsonStructData = dbtCloudClient.GetProjects(listFilterProjects)

			case "dbtcloud_project_repository":
				allProjectsRepositories := dbtCloudClient.GetProjects(listFilterProjects)
				jsonStructData = lo.Filter(allProjectsRepositories, func(project any, idx int) bool {
					projectTyped := project.(map[string]any)
					return projectTyped["repository_id"] != nil
				})

			case "dbtcloud_job":
				jsonStructData = prefetchedJobs

			case "dbtcloud_environment":
				jsonStructData = dbtCloudClient.GetEnvironments(listFilterProjects)

			case "dbtcloud_environment_variable":
				mapEnvVars := dbtCloudClient.GetEnvironmentVariables(listFilterProjects)

				listEnvVars := []any{}
				for projectID, envVars := range mapEnvVars {
					for envVarName := range envVars.(map[string]any) {
						envDetails := map[string]any{}
						envDetails["name"] = envVarName
						envDetails["project_id"] = float64(projectID)
						envDetails["id"] = fmt.Sprintf("%d_%s", projectID, envVarName)
						listEnvVars = append(listEnvVars, envDetails)
					}
				}
				jsonStructData = listEnvVars

			case "dbtcloud_group":
				// TODO add removal of default groups to the API call side
				allGroups := dbtCloudClient.GetGroups()

				listGroups := []any{}
				for _, group := range allGroups {
					groupTyped := group.(map[string]any)

					defaultGroups := []string{"Owner", "Member", "Everyone"}

					// we check if the group is one of the default ones
					_, found := lo.Find(defaultGroups, func(i string) bool {
						return i == groupTyped["name"].(string)
					})
					// only add if the current group doesn't match with the default ones
					if !found {
						listGroups = append(listGroups, group)
					}
				}
				jsonStructData = listGroups

			case "dbtcloud_snowflake_credential":
				jsonStructData = dbtCloudClient.GetSnowflakeCredentials(listFilterProjects)

			case "dbtcloud_databricks_credential":
				jsonStructData = dbtCloudClient.GetDatabricksCredentials(listFilterProjects)

			case "dbtcloud_bigquery_credential":
				jsonStructData = dbtCloudClient.GetBigQueryCredentials(listFilterProjects)

			case "dbtcloud_repository":
				jsonStructData = dbtCloudClient.GetRepositories(listFilterProjects)

			case "dbtcloud_bigquery_connection":
				jsonStructData = dbtCloudClient.GetBigQueryConnections(listFilterProjects)

			case "dbtcloud_connection":
				jsonStructData = dbtCloudClient.GetGenericConnections(listFilterProjects)

			case "dbtcloud_extended_attributes":
				jsonStructData = dbtCloudClient.GetExtendedAttributes(listFilterProjects)

			case "dbtcloud_user_groups":
				jsonStructData = dbtCloudClient.GetUsers()

			case "dbtcloud_webhook":
				allWebHooks := dbtCloudClient.GetWebhooks()
				jsonStructData = lo.Filter(allWebHooks, func(webhook any, idx int) bool {
					webhookTyped := webhook.(map[string]any)
					listJobIDs := webhookTyped["job_ids"].([]any)
					if len(listJobIDs) == 0 {
						// if there is no job defined, then the webhook is for all jobs
						return true
					}
					return len(lo.Intersect(listJobIDs, prefetchedJobsIDsAny)) > 0
				})

			case "dbtcloud_notification":
				allNotifications := dbtCloudClient.GetNotifications()
				jsonStructData = lo.Filter(allNotifications, func(notif any, idx int) bool {
					notifTyped := notif.(map[string]any)
					return !(notifTyped["type"].(float64) == 4 && notifTyped["external_email"] == nil)

				})
			case "dbtcloud_service_token":
				jsonStructData = dbtCloudClient.GetServiceTokens()

			case "dbtcloud_global_connection":
				jsonStructData = dbtCloudClient.GetGlobalConnectionsSummary()

			default:
				fmt.Fprintf(cmd.OutOrStderr(), "%q is not yet supported for state import", resourceType)
				return
			}

			for _, data := range jsonStructData {
				var idStr string
				switch id := data.(map[string]interface{})["id"].(type) {
				case float64:
					idStr = fmt.Sprintf("%.0f", id)
				case string:
					idStr = id
				default:
					// Handle other unexpected types
				}

				if useModernImportBlock {
					idvalue := buildRawImportAddress(resourceType, idStr, data)
					imp := importBody.AppendNewBlock("import", []string{}).Body()
					imp.SetAttributeRaw("to", hclwrite.TokensForIdentifier(fmt.Sprintf("%s.%s", resourceType, fmt.Sprintf("%s_%s", terraformResourceNamePrefix, idStr))))
					imp.SetAttributeValue("id", cty.StringVal(idvalue))
					importFile.Body().AppendNewline()
				} else {
					if err := writeString(buildTerraformImportCommand(resourceType, idStr, data)); err != nil {
						log.Fatalf("failed to write import command: %v", err)
					}
				}
			}

		}
		if useModernImportBlock {
			if err := writeOutput(importFile); err != nil {
				log.Fatalf("failed to write import blocks: %v", err)
			}
		}
	}
}

// buildTerraformImportCommand takes the resourceType and resourceID in order to
// lookup the resource type import string and then return a suitable composite
// value that is compatible with `terraform import`.
func buildTerraformImportCommand(resourceType, resourceID string, data interface{}) string {
	resourceImportAddress := buildRawImportAddress(resourceType, resourceID, data)
	return fmt.Sprintf("%s %s.%s_%s %s\n", terraformImportCmdPrefix, resourceType, terraformResourceNamePrefix, resourceID, resourceImportAddress)
}

// buildRawImportAddress takes the resourceType and resourceID in order to lookup the
// resource type import string and then return a suitable composite value that
// is compatible with `terraform import`.
func buildRawImportAddress(resourceType, resourceID string, data any) string {
	if _, ok := resourceImportStringFormats[resourceType]; !ok {
		log.Fatalf("%s does not have an import format defined", resourceType)
	}

	var identifierType string
	var identifierValue string

	identifierType = "account"
	identifierValue = accountID

	connectionIDRaw, ok := data.(map[string]any)["connection_id"]
	var connectionID string
	if !ok {
		connectionID = "no-connection_id"
	} else {
		connectionIDFloat, ok := connectionIDRaw.(float64)
		if !ok {
			connectionID = "no-connection_id"
		} else {
			connectionID = fmt.Sprintf("%0.f", connectionIDFloat)
		}
	}

	repositoryIDCasted, ok := data.(map[string]any)["repository_id"].(float64)
	var repositoryID string
	if !ok {
		repositoryID = "no-repository_id"
	} else {
		repositoryID = fmt.Sprintf("%0.f", repositoryIDCasted)
	}

	projectIDRaw, ok := data.(map[string]any)["project_id"]
	var projectID string
	if !ok {
		projectID = "no-project_id"
	} else {
		projectID = fmt.Sprintf("%0.f", projectIDRaw.(float64))
	}

	nameRaw, ok := data.(map[string]any)["name"]
	var name string
	if !ok {
		name = "no-name"
	} else {
		name = nameRaw.(string)
	}

	var userID string
	if resourceType == "dbtcloud_user_groups" {
		// for dbtcloud_user_groups, the ID is the user ID
		userIDRaw, ok := data.(map[string]any)["id"]
		_ = userIDRaw
		if !ok {
			userID = "no-userid"
		} else {
			userID = fmt.Sprintf("%0.f", userIDRaw.(float64))
		}
	}

	s := resourceImportStringFormats[resourceType]
	replacer := strings.NewReplacer(
		":identifier_type", identifierType,
		":identifier_value", identifierValue,
		":zone_id", zoneID,
		":account_id", accountID,
		":id", resourceID,
		":connection_id", connectionID,
		":repository_id", repositoryID,
		":project_id", projectID,
		":name", name,
		":user_id", userID,
	)

	return replacer.Replace(s)
}
