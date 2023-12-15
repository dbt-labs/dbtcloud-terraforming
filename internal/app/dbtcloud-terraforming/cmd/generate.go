package cmd

import (
	"context"
	"os"
	"sort"

	"github.com/dbt-cloud/dbtcloud-terraforming/dbtcloud"
	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zclconf/go-cty/cty"

	"fmt"
)

var resourceTypes []string
var linkResources = true

func init() {
	rootCmd.AddCommand(generateCmd)
}

var generateCmd = &cobra.Command{
	Use:    "generate",
	Short:  "Fetch resources from the dbt Cloud API and generate the respective Terraform stanzas",
	Run:    generateResources(),
	PreRun: sharedPreRun,
}

func generateResources() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if resourceTypes == nil {
			log.Fatal("you must define a resource type to generate")
		}

		accountID = viper.GetString("account")
		apiToken = viper.GetString("token")
		hostname = viper.GetString("hostname")
		if hostname == "" {
			hostname = "cloud.getdbt.com"
		}
		listFilterProjects = viper.GetIntSlice("projects")

		config := dbtcloud.DbtCloudConfig{
			Hostname:  hostname,
			APIToken:  apiToken,
			AccountID: accountID,
		}

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

		for _, resourceType := range resourceTypes {

			r := s.ResourceSchemas[resourceType]
			log.Debugf("beginning to read and build %s resources", resourceType)

			// Initialise `resourceCount` outside of the switch for supported resources
			// to allow it to be referenced further down in the loop that outputs the
			// newly generated resources.
			resourceCount := 0

			// Lazy approach to restrict support to known resources due to Go's type
			// restrictions and the need to explicitly map out the structs.

			// type DataResult struct {
			// 	Result []interface{}
			// }

			// type Answer struct {
			// 	Data []DataResult `json:"data"`
			// }

			// type Response struct {
			// 	Data []interface{} `json:"data"`
			// }

			// var answer DataWrapper
			// var jsonStructData []interface{}

			// var identifier *cloudflare.ResourceContainer
			// if accountID != "" {
			// 	identifier = cloudflare.AccountIdentifier(accountID)
			// } else {
			// 	identifier = cloudflare.ZoneIdentifier(zoneID)
			// }
			var jsonStructData []any

			switch resourceType {
			case "dbtcloud_project":

				jsonStructData = dbtcloud.GetProjects(config)
				resourceCount = len(jsonStructData)

			case "dbtcloud_project_connection":

				listProjects := dbtcloud.GetProjects(config)

				for _, project := range listProjects {
					projectTyped := project.(map[string]any)
					projectTyped["project_id"] = projectTyped["id"].(float64)
					jsonStructData = append(jsonStructData, projectTyped)

					if linkResources {
						projectID := projectTyped["project_id"]
						projectTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)
					}
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_job":

				jobs := dbtcloud.GetJobs(config)

				for _, job := range jobs {
					jobTyped := job.(map[string]any)

					projectID := jobTyped["project_id"].(float64)
					if len(listFilterProjects) > 0 && lo.Contains(listFilterProjects, int(projectID)) == false {
						continue
					}

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
					if jobTyped["schedule_type"] == "days_of_week" {
						jobTyped["schedule_days"] = jobScheduleDate["days"]

						jobScheduleTime := jobSchedule["time"].(map[string]any)
						if jobScheduleTime["type"].(string) == "at_exact_hours" {
							jobTyped["schedule_hours"] = jobScheduleTime["hours"]
						}

						// TODO: Handle the case when this is every x hours
					}

					jobTriggers := jobTyped["triggers"].(map[string]any)

					triggers := map[string]any{
						"github_webhook":       jobTriggers["github_webhook"].(bool),
						"git_provider_webhook": jobTriggers["git_provider_webhook"].(bool),
						"custom_branch_only":   false,
						"schedule":             jobTriggers["schedule"].(bool),
					}

					jobTyped["triggers"] = triggers

					if linkResources {
						environmentID := jobTyped["environment_id"].(float64)
						jobTyped["environment_id"] = fmt.Sprintf("dbtcloud_environment.terraform_managed_resource_%0.f.environment_id", environmentID)

						projectID := jobTyped["project_id"].(float64)
						jobTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)
					}
					jsonStructData = append(jsonStructData, jobTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_environment":

				listEnvironments := dbtcloud.GetEnvironments(config)

				for _, environment := range listEnvironments {
					environmentsTyped := environment.(map[string]any)
					projectID := environmentsTyped["project_id"].(float64)

					// we skip if a project list is defined and the project is not in the list
					if len(listFilterProjects) > 0 && lo.Contains(listFilterProjects, int(projectID)) == false {
						continue
					}

					if linkResources {
						environmentsTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)
					}

					// handle the case when credentials_id is not a float because it is null
					if credentials_id, ok := environmentsTyped["credentials_id"].(float64); ok {
						environmentsTyped["credential_id"] = credentials_id
					}

					jsonStructData = append(jsonStructData, environmentsTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_repository":

				listRepositories := dbtcloud.GetRepositories(config)

				for _, repository := range listRepositories {
					repositoryTyped := repository.(map[string]any)

					projectID := repositoryTyped["project_id"].(float64)
					if len(listFilterProjects) > 0 && lo.Contains(listFilterProjects, int(projectID)) == false {
						continue
					}

					if linkResources {
						repositoryTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)
					}
					jsonStructData = append(jsonStructData, repositoryTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_project_repository":

				listProjects := dbtcloud.GetProjects(config)

				for _, project := range listProjects {
					projectTyped := project.(map[string]any)
					projectID := projectTyped["id"].(float64)
					projectTyped["project_id"] = projectID
					if len(listFilterProjects) > 0 && lo.Contains(listFilterProjects, int(projectID)) == false {
						continue
					}
					jsonStructData = append(jsonStructData, projectTyped)

					if linkResources {
						projectID := projectTyped["project_id"]
						projectTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)

						repositoryID := projectTyped["repository_id"]
						projectTyped["repository_id"] = fmt.Sprintf("dbtcloud_repository.terraform_managed_resource_%0.f.repository_id", repositoryID)
					}
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_group":

				listGroups := dbtcloud.GetGroups(config)

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

					if linkResources {

						groupPermissions, ok := groupTyped["group_permissions"].([]any)
						if !ok {
							panic("Could not cast group_permissions to []any")
						}
						newGroupPermissionsTyped := []map[string]any{}
						for _, groupPermission := range groupPermissions {
							groupPermissionTyped := groupPermission.(map[string]any)
							if groupPermissionTyped["all_projects"] == false {
								groupPermissionTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", groupPermissionTyped["project_id"].(float64))
							}
							newGroupPermissionsTyped = append(newGroupPermissionsTyped, groupPermissionTyped)
						}
						groupTyped["group_permissions"] = newGroupPermissionsTyped

					}
					jsonStructData = append(jsonStructData, group)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_environment_variable":

				mapEnvVars := dbtcloud.GetEnvironmentVariables(config, listFilterProjects)
				listEnvVars := []any{}

				for projectID, envVars := range mapEnvVars {
					for envVarName, envVarValues := range envVars.(map[string]any) {
						envDetails := map[string]any{}
						envDetails["name"] = envVarName
						envDetails["id"] = fmt.Sprintf("%d_%s", projectID, envVarName)
						envDetails["project_id"] = projectID

						if linkResources {
							envDetails["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%d.id", projectID)
							// TODO: Add the hard coded dependencies with the environments
						}

						// we need to make int a map[string]any to work with the matching strategy
						collectEnvValues := map[string]any{}

						envVarValuesTyped := envVarValues.(map[string]any)
						for envName, envValues := range envVarValuesTyped {

							if envValues != nil {
								envValuesTyped := envValues.(map[string]any)
								collectEnvValues[envName] = envValuesTyped["value"].(string)
								log.Warn(envName, envValues)
							}

						}
						envDetails["environment_values"] = collectEnvValues

						listEnvVars = append(listEnvVars, envDetails)
					}
				}

				jsonStructData = listEnvVars
				resourceCount = len(jsonStructData)

			case "dbtcloud_snowflake_credential":
				listCredentials := dbtcloud.GetSnowflakeCredentials(config)

				for _, credential := range listCredentials {
					credentialTyped := credential.(map[string]any)

					// we filter the correct projects if need be
					projectID := credentialTyped["project_id"].(float64)
					if len(listFilterProjects) > 0 && lo.Contains(listFilterProjects, int(projectID)) == false {
						continue
					}

					credentialTyped["num_threads"] = credentialTyped["threads"]

					switch credentialTyped["auth_type"] {
					case "password":
						credentialTyped["password"] = "---TBD---"
					case "keypair":
						credentialTyped["private_key"] = "!!!TBD!!!"
						credentialTyped["private_key_passphrase"] = "---TBD---"
					}

					if linkResources {
						credentialTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)
					}
					jsonStructData = append(jsonStructData, credentialTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_bigquery_credential":
				listCredentials := dbtcloud.GetBigQueryCredentials(config)

				for _, credential := range listCredentials {
					credentialTyped := credential.(map[string]any)

					// we filter the correct projects if need be
					projectID := credentialTyped["project_id"].(float64)
					if len(listFilterProjects) > 0 && lo.Contains(listFilterProjects, int(projectID)) == false {
						continue
					}

					credentialTyped["num_threads"] = credentialTyped["threads"]
					credentialTyped["dataset"] = credentialTyped["schema"]

					if linkResources {
						credentialTyped["project_id"] = fmt.Sprintf("dbtcloud_project.terraform_managed_resource_%0.f.id", projectID)
					}
					jsonStructData = append(jsonStructData, credentialTyped)
				}

				resourceCount = len(jsonStructData)

			case "dbtcloud_bigquery_connection":
				bigqueryConnections := dbtcloud.GetBigQueryConnections(config, listFilterProjects)
				bigqueryConnectionsTyped := []any{}

				for _, connection := range bigqueryConnections {
					connectionTyped := connection.(map[string]any)
					connectionDetailsTyped := connectionTyped["details"].(map[string]any)

					// we "promote" all details fields one level up like in the Terraform resource
					for detailKey, detailVal := range connectionDetailsTyped {
						connectionTyped[detailKey] = detailVal
					}

					// we add the secure fields
					connectionTyped["private_key"] = "---TBD---"
					connectionTyped["application_id"] = "---TBD if using OAuth, otherwise delete---"
					connectionTyped["private_key"] = "---TBD if using OAuth, otherwise delete---"

					bigqueryConnectionsTyped = append(bigqueryConnectionsTyped, connectionTyped)
				}

				jsonStructData = bigqueryConnectionsTyped
				resourceCount = len(jsonStructData)

			default:
				fmt.Fprintf(cmd.OutOrStderr(), "%q is not yet supported for automatic generation", resourceType)
				return
			}
			// If we don't have any resources to generate, just bail out early.
			if resourceCount == 0 {
				fmt.Fprint(cmd.OutOrStderr(), "no resources found to generate. Exiting...")
				return
			}

			f := hclwrite.NewEmptyFile()
			rootBody := f.Body()
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
						panic("There is no `id` defined for the resources")
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
				f.Body().AppendNewline()
			}

			tfOutput := string(hclwrite.Format(f.Bytes()))
			fmt.Fprint(cmd.OutOrStdout(), tfOutput)
		}
	}
}
