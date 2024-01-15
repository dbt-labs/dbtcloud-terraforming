# dbt Cloud Terraforming

```sh
dbtcloud-terraforming is an application that allows dbt Cloud users
to be able to adopt Terraform by giving them a feasible way to get
all of their existing dbt Cloud configuration into Terraform.

Usage:
  dbtcloud-terraforming [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  generate    Fetch resources from the dbt Cloud API and generate the respective Terraform stanzas
  help        Help about any command
  import      Output `terraform import` compatible commands and/or import blocks (require terraform >= 1.5) in order to import resources into state
  version     Print the version number of dbtcloud-terraforming

Flags:
  -a, --account string                  Use specific account ID for commands
  -h, --help                            help for dbtcloud-terraforming
      --host-url string                 Host URL to use to query the API, includes the /api part
      --linked-resource-types strings   List of resource types to make dependencies links to instead of using IDs. Can be set to all for linking all resources.
      --modern-import-block             Whether to generate HCL import blocks for generated resources instead of terraform import compatible CLI commands. This is only compatible with Terraform 1.5+
  -p, --projects ints                   Project IDs to limit the import for
      --resource-types strings          List of resource types you wish to generate
      --terraform-binary-path string    Path to an existing Terraform binary (otherwise, one will be downloaded)
      --terraform-install-path string   Path to an initialized Terraform working directory (default ".")
  -t, --token string                    API Token
  -v, --verbose                         Specify verbose output (same as setting log level to debug)
```

This tool can be used to load existing dbt Cloud configuration into Terraform. Currently the following resources are supported:

| Resource                                   | Resource Scope | Generate Supported | Import Supported | Requires manual setup |
| ------------------------------------------ | -------------- | ------------------ | ---------------- | --------------------- |
| dbtcloud_bigquery_connection               | Project        | ✅                 | ✅               | 🔒                    |
| dbtcloud_bigquery_credential               | Project        | ✅                 | ✅               |                       |
| dbtcloud_connection                        | Project        | ✅                 | ✅               | 🔒*                   |
| dbtcloud_databricks_credential             | Project        |                    |                  |                       |
| dbtcloud_environment                       | Project        | ✅                 | ✅               |                       |
| dbtcloud_environment_variable              | Project        | ✅                 | ✅               |                       |
| dbtcloud_environment_variable_job_override | Project        |                    |                  |                       |
| dbtcloud_extended_attributes(*)            | Project        | ✅                 | ✅               |                       |
| dbtcloud_fabric_connection                 | Project        |                    |                  |                       |
| dbtcloud_fabric_credential                 | Project        |                    |                  | 🔒                    |
| dbtcloud_group                             | Account        | ✅                 | ✅               |                       |
| dbtcloud_job                               | Project        | ✅                 | ✅               |                       |
| dbtcloud_license_map                       | Account        |                    |                  |                       |
| dbtcloud_notification                      | Account        |                    |                  |                       |
| dbtcloud_postgres_credential               | Project        |                    |                  | 🔒*                   |
| dbtcloud_project                           | Project        | ✅                 | ✅               |                       |
| dbtcloud_project_artefacts                 | Project        |                    |                  |                       |
| dbtcloud_project_connection                | Project        | ✅                 | ✅               |                       |
| dbtcloud_project_repository                | Project        | ✅                 | ✅               |                       |
| dbtcloud_repository                        | Project        | ✅                 | ✅               |                       |
| dbtcloud_service_token                     | Account        |                    |                  | 🔒                    |
| dbtcloud_snowflake_credential              | Project        | ✅                 | ✅               | 🔒                    |
| dbtcloud_user_groups                       | Account        |                    |                  |                       |
| dbtcloud_webhook                           | Account        |                    |                  |                       |

Notes:

- `dbtcloud_connection` is supported for Snowflake, Redshift, Postgres and Databricks, but not for Spark
- `dbtcloud_extended_attributes` currently doesn't generate config for nested fields, only top level ones

## How to use the tool

### Connecting to dbt Cloud

To connect to dbt Cloud, you need to provide an API token and a dbt Cloud Account ID. If your account is not hosted on cloud.getdbt.com you will also need to provide the relevant API endpoint.

- token: can be set in the CLI with `--token` or `-t` ; or setting up the env var `DBT_CLOUD_TOKEN`
- account id: can be set in the CLI with `--account` or `-a` ; or setting up the env var `DBT_CLOUD_ACCOUNT_ID`
- API Host URL: can be set in the CLI with `--host-url` or setting up the env var `DBT_CLOUD_HOST_URL`

Example:

```sh
export DBT_CLOUD_TOKEN=<token>
export DBT_CLOUD_ACCOUNT_ID=123
export DBT_CLOUD_HOST_URL="http://emea.dbt.com/api"
```

### Executing the tool

Download the tool and run commands like below:

To generate the config

```sh
dbtcloud-terraforming generate --resource-types dbcloud_project,dbtcloud_environment,dbtcloud_job --linked-resource-types dbtcloud_project,dbtcloud_environment
```

To generate the import blocks

```sh
dbtcloud-terraforming import --resource-types dbcloud_project,dbtcloud_environment,dbtcloud_job --modern-import-block
```

Once both of the outputs are generated, you can copy paste them in a terraform file having the `dbtcloud` provider already set up and you can run a `terraform plan`.
You should see that all the resources are going to be imported and that no change will be triggered.

The different `resource_types` that can be used are the ones from the table above. They are a subset of the resources available in the dbt Cloud Terraform provider.
Generating and importing multiple resource types at once is possible by separating them with `,`

### Selecting specific projects

By default, the tool loads all projects but we can restrict the projects to focus on by selecting `--projects 123,456,789` with 123, 456 and 789 being the projects we want to load in Terraform

### Linking resources in the configuration

When generating the configuration, if `--linked-resource-types` is not set for any resource, all the existing IDs (Project ID, Environment ID etc...) will be stored in the config as IDs.

This is OK to load the configuration in Terraform, but it also means that if those projects change on the dbt Cloud side, Terraform will start failing.
Additionally, if a project is getting deleted for example, Terraform won't understand the impact to other objects, like environments, and you will start seeing errors.

Setting up `--linked-resource-types` with the resource types you want to link will "link" resources together, like in a typical Terraform configuration.

With this example:

```sh
dbtcloud-terraforming generate --resource-types dbcloud_project,dbtcloud_environment,dbtcloud_job --linked-resource-types dbtcloud_project,dbtcloud_environment
```

- the dbt Cloud environment resources will be linked to the project
- and the dbt Cloud jobs will be linked to both their relevant project and environment

This can be especially useful if you want to replicate an existing project. To do so, you can generate all the config *without* importing it. You could change the name of a project, and after running a `terraform apply` all the objects will be newly created, replicating your existing config in another project.

## Credits

A big part of this tool has been inspired from the Cloudflare library [cf-terraforming](https://github.com/cloudflare/cf-terraforming/tree/master)
