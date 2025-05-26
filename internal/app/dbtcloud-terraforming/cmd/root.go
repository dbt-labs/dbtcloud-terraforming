package cmd

import (
	"github.com/dbt-labs/dbtcloud-terraforming/dbtcloud"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var log = logrus.New()
var zoneID, hostURL, apiToken, accountID, terraformInstallPath, terraformBinaryPath string
var listFilterProjects []int
var verbose, useModernImportBlock, parameterizeJobs bool
var dbtCloudClient *dbtcloud.DbtCloudHTTPClient
var terraformImportCmdPrefix = "terraform import"
var terraformResourceNamePrefix = "terraform_managed_resource"

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "dbtcloud-terraforming",
	Short: "Bootstrapping Terraform from existing dbt Cloud account",
	Long: `dbtcloud-terraforming is an application that allows dbt Cloud users
to be able to adopt Terraform by giving them a feasible way to get
all of their existing dbt Cloud configuration into Terraform.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Error(err)
		return
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	var err error

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// Output file
	rootCmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "", "Output file path. If not specified, output is written to stdout")
	if err = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output")); err != nil {
		log.Fatal(err)
	}

	// Account
	rootCmd.PersistentFlags().StringVarP(&accountID, "account", "a", "", "Use specific account ID for commands. [env var: DBT_CLOUD_ACCOUNT_ID]")
	if err = viper.BindPFlag("account", rootCmd.PersistentFlags().Lookup("account")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("account", "DBT_CLOUD_ACCOUNT_ID"); err != nil {
		log.Fatal(err)
	}

	// API credentials

	rootCmd.PersistentFlags().StringVarP(&apiToken, "token", "t", "", "API Token. [env var: DBT_CLOUD_TOKEN]")
	if err = viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("token", "DBT_CLOUD_TOKEN"); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().StringVarP(&hostURL, "host-url", "", "", "Host URL to use to query the API, includes the /api part. [env var: DBT_CLOUD_HOST_URL]")
	if err = viper.BindPFlag("host-url", rootCmd.PersistentFlags().Lookup("host-url")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("host-url", "DBT_CLOUD_HOST_URL"); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().IntSliceVarP(&listFilterProjects, "projects", "p", []int{}, "Project IDs to limit the import for. Imports all projects if not set. [env var: DBT_CLOUD_PROJECTS]")
	if err = viper.BindPFlag("projects", rootCmd.PersistentFlags().Lookup("projects")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("projects", "DBT_CLOUD_PROJECTS"); err != nil {
		log.Fatal(err)
	}

	// Debug logging mode
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Specify verbose output (same as setting log level to debug)")

	rootCmd.PersistentFlags().StringSliceVar(&resourceTypes, "resource-types", []string{}, "List of resource types you wish to generate. Use `all` to generate all resources")

	rootCmd.PersistentFlags().StringSliceVar(&excludeResourceTypes, "exclude-resource-types", []string{}, "List of resource types you wish to exclude from the generation. To be used with --resource-types all")

	rootCmd.PersistentFlags().StringSliceVar(&listLinkedResources, "linked-resource-types", []string{}, "List of resource types to make dependencies links to instead of using IDs. Can be set to 'all' for linking all resources")

	rootCmd.PersistentFlags().BoolVarP(&useModernImportBlock, "modern-import-block", "", true, "Whether to generate HCL import blocks for generated resources instead of terraform import compatible CLI commands. This is only compatible with Terraform 1.5+.")

	rootCmd.PersistentFlags().StringVar(&terraformInstallPath, "terraform-install-path", ".", "Path to an initialized Terraform working directory [env var: DBT_CLOUD_TERRAFORM_INSTALL_PATH]")

	if err = viper.BindPFlag("terraform-install-path", rootCmd.PersistentFlags().Lookup("terraform-install-path")); err != nil {
		log.Fatal(err)
	}

	if err = viper.BindEnv("terraform-install-path", "DBT_CLOUD_TERRAFORM_INSTALL_PATH"); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().StringVar(&terraformBinaryPath, "terraform-binary-path", "", "Path to an existing Terraform binary (otherwise, one will be downloaded) [env var: DBT_CLOUD_TERRAFORM_BINARY_PATH]")

	if err = viper.BindPFlag("terraform-binary-path", rootCmd.PersistentFlags().Lookup("terraform-binary-path")); err != nil {
		log.Fatal(err)
	}

	if err = viper.BindEnv("terraform-binary-path", "DBT_CLOUD_TERRAFORM_BINARY_PATH"); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().BoolVarP(&parameterizeJobs, "parameterize-jobs", "", false, "Whether to parameterize jobs. Default=false")
}

// initConfig reads ENV variables if set.
func initConfig() {

	viper.AutomaticEnv() // read in environment variables that match
	viper.SetEnvPrefix("dbtcloud_terraforming")

	var cfgLogLevel = logrus.InfoLevel

	if verbose {
		cfgLogLevel = logrus.DebugLevel
	}

	log.SetLevel(cfgLogLevel)
}
