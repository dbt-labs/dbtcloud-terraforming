package cmd

import (
	"fmt"
	"strings"

	"github.com/dbt-cloud/dbtcloud-terraforming/dbtcloud"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
}

func init() {
	rootCmd.AddCommand(importCommand)
}

var importCommand = &cobra.Command{
	Use:    "import",
	Short:  "Output `terraform import` compatible commands in order to import resources into state",
	Run:    runImport(),
	PreRun: sharedPreRun,
}

func runImport() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		var jsonStructData []interface{}

		accountID = viper.GetString("account")
		apiToken = viper.GetString("token")
		hostname = viper.GetString("hostname")
		if hostname == "" {
			hostname = "cloud.getdbt.com"
		}

		config := dbtcloud.DbtCloudConfig{
			Hostname:  hostname,
			APIToken:  apiToken,
			AccountID: accountID,
		}

		switch resourceType {

		case "dbtcloud_project":
			jsonStructData = dbtcloud.GetProjects(config)

		case "dbtcloud_project_connection":
			jsonStructData = dbtcloud.GetProjects(config)

		case "dbtcloud_project_repository":
			jsonStructData = dbtcloud.GetProjects(config)

		case "dbtcloud_job":
			jsonStructData = dbtcloud.GetJobs(config)

		case "dbtcloud_environment":
			jsonStructData = dbtcloud.GetEnvironments(config)

		case "dbtcloud_environment_variable":
			mapEnvVars := dbtcloud.GetEnvironmentVariables(config, listFilterProjects)

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
			jsonStructData = dbtcloud.GetGroups(config)

		case "dbtcloud_snowflake_credential":
			jsonStructData = dbtcloud.GetSnowflakeCredentials(config)

		case "dbtcloud_repository":
			jsonStructData = dbtcloud.GetRepositories(config)

		default:
			fmt.Fprintf(cmd.OutOrStdout(), "%q is not yet supported for state import", resourceType)
			return
		}

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

			fmt.Fprint(cmd.OutOrStdout(), buildCompositeID(resourceType, idStr, data))

			// fmt.Fprint(cmd.OutOrStdout(), buildCompositeID(resourceType, data.(map[string]interface{})["id"].(string)))
		}
	}
}

// buildCompositeID takes the resourceType and resourceID in order to lookup the
// resource type import string and then return a suitable composite value that
// is compatible with `terraform import`.
func buildCompositeID(resourceType, resourceID string, data interface{}) string {
	if _, ok := resourceImportStringFormats[resourceType]; !ok {
		log.Fatalf("%s does not have an import format defined", resourceType)
	}

	var identiferType string
	var identiferValue string

	if accountID != "" {
		identiferType = "account"
		identiferValue = accountID
	} else {
		identiferType = "zone"
		identiferValue = zoneID
	}

	s := ""
	s += fmt.Sprintf("%s %s.%s_%s %s", terraformImportCmdPrefix, resourceType, terraformResourceNamePrefix, resourceID, resourceImportStringFormats[resourceType])

	connnectionIDRaw, ok := data.(map[string]interface{})["connection_id"]
	var connectionID string
	if !ok {
		connectionID = "no-connection_id"
	} else {
		connectionIDFloat, ok := connnectionIDRaw.(float64)
		if !ok {
			connectionID = "no-connection_id"
		} else {
			connectionID = fmt.Sprintf("%0.f", connectionIDFloat)
		}
	}

	repositoryIDRaw, ok := data.(map[string]interface{})["repository_id"]
	var repositoryID string
	if !ok {
		repositoryID = "no-repository_id"
	} else {
		repositoryID = fmt.Sprintf("%0.f", repositoryIDRaw.(float64))
	}

	projectIDRaw, ok := data.(map[string]interface{})["project_id"]
	var projectID string
	if !ok {
		projectID = "no-project_id"
	} else {
		projectID = fmt.Sprintf("%0.f", projectIDRaw.(float64))
	}

	nameRaw, ok := data.(map[string]interface{})["name"]
	var name string
	if !ok {
		name = "no-name"
	} else {
		name = nameRaw.(string)
	}

	replacer := strings.NewReplacer(
		":identifier_type", identiferType,
		":identifier_value", identiferValue,
		":zone_id", zoneID,
		":account_id", accountID,
		":id", resourceID,
		":connection_id", connectionID,
		":repository_id", repositoryID,
		":project_id", projectID,
		":name", name,
	)
	s += "\n"

	return replacer.Replace(s)
}
