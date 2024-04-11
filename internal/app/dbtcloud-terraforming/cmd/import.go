package cmd

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zclconf/go-cty/cty"
)

// resourceImportStringFormats contains a mapping of the resource type to the
// composite ID that is compatible with performing an import.
var resourceImportStringFormats = map[string]string{
	"dbtcloud_project":              ":id",
	"dbtcloud_project_connection":   ":id::connection_id",
	"dbtcloud_repository":           ":project_id::id",
	"dbtcloud_project_repository":   ":id::repository_id",
	"dbtcloud_job":                  ":id",
	"dbtcloud_environment":          ":project_id::id",
	"dbtcloud_environment_variable": ":project_id::name",
	"dbtcloud_group":                ":id",
	"dbtcloud_snowflake_credential": ":project_id::id",
	"dbtcloud_bigquery_credential":  ":project_id::id",
	"dbtcloud_bigquery_connection":  ":project_id::id",
	"dbtcloud_connection":           ":project_id::id",
	"dbtcloud_extended_attributes":  ":project_id::id",
	"dbtcloud_user_groups":          ":user_id",
	"dbtcloud_webhook":              ":id",
	"dbtcloud_notification":         ":id",
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

		for _, resourceType := range resourceTypes {

			switch resourceType {

			case "dbtcloud_project":
				jsonStructData = dbtCloudClient.GetProjects(listFilterProjects)

			case "dbtcloud_project_connection":
				jsonStructData = dbtCloudClient.GetProjects(listFilterProjects)

			case "dbtcloud_project_repository":
				jsonStructData = dbtCloudClient.GetProjects(listFilterProjects)

			case "dbtcloud_job":
				jsonStructData = dbtCloudClient.GetJobs(listFilterProjects)

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
				jsonStructData = dbtCloudClient.GetWebhooks()

			case "dbtcloud_notification":
				jsonStructData = dbtCloudClient.GetNotifications()

			default:
				fmt.Fprintf(cmd.OutOrStderr(), "%q is not yet supported for state import", resourceType)
				return
			}

			importFile := hclwrite.NewEmptyFile()
			importBody := importFile.Body()

			for _, data := range jsonStructData {

				var idStr string
				switch id := data.(map[string]interface{})["id"].(type) {
				case float64:
					// Convert float64 to string, assuming you want to truncate to an integer
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
					fmt.Fprint(cmd.OutOrStdout(), buildTerraformImportCommand(resourceType, idStr, data))
				}
			}
			if useModernImportBlock {

				// don't format the output; there is a bug in hclwrite.Format that
				// splits incorrectly on certain characters. instead, manually
				// insert new lines on the block.
				fmt.Fprint(cmd.OutOrStdout(), string(importFile.Bytes()))

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
