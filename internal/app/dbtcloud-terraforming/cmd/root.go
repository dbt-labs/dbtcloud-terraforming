package cmd

import (
	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/dbt-cloud/dbtcloud-terraforming/dbtcloud"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var log = logrus.New()
var cfgFile, zoneID, hostURL, apiEmail, apiKey, apiToken, accountID, terraformInstallPath, terraformBinaryPath string
var listFilterProjects []int
var verbose, useModernImportBlock bool
var api *cloudflare.API
var dbtCloudClient *dbtcloud.DbtCloudHTTPClient
var terraformImportCmdPrefix = "terraform import"
var terraformResourceNamePrefix = "terraform_managed_resource"

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "dbtcloud-terraforming",
	Short: "Bootstrapping Terraform from existing Cloudflare account",
	Long: `dbtcloud-terraforming is an application that allows Cloudflare users
to be able to adopt Terraform by giving them a feasible way to get
all of their existing Cloudflare configuration into Terraform.`,
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

	home, err := homedir.Dir()
	if err != nil {
		log.Debug(err)
		return
	}

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", home+"/.dbtcloud-terraforming.yaml", "Path to config file")

	// Account
	rootCmd.PersistentFlags().StringVarP(&accountID, "account", "a", "", "Use specific account ID for commands")
	if err = viper.BindPFlag("account", rootCmd.PersistentFlags().Lookup("account")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("account", "DBT_CLOUD_ACCOUNT_ID"); err != nil {
		log.Fatal(err)
	}

	// API credentials

	rootCmd.PersistentFlags().StringVarP(&apiToken, "token", "t", "", "API Token")
	if err = viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("token", "DBT_CLOUD_TOKEN"); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().StringVarP(&hostURL, "host-url", "", "", "Host URL to use to query the API, includes the /api part")
	if err = viper.BindPFlag("host-url", rootCmd.PersistentFlags().Lookup("host-url")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("host-url", "DBT_CLOUD_HOST_URL"); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().IntSliceVarP(&listFilterProjects, "projects", "p", []int{}, "Project IDs to limit the import for")
	if err = viper.BindPFlag("projects", rootCmd.PersistentFlags().Lookup("projects")); err != nil {
		log.Fatal(err)
	}
	if err = viper.BindEnv("projects", "DBT_CLOUD_PROJECTS"); err != nil {
		log.Fatal(err)
	}

	// Debug logging mode
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Specify verbose output (same as setting log level to debug)")

	rootCmd.PersistentFlags().StringSliceVar(&resourceTypes, "resource-types", []string{}, "List of resource types you wish to generate")

	rootCmd.PersistentFlags().StringSliceVar(&listLinkedResources, "linked-resource-types", []string{}, "List of resource types to make dependencies links to instead of using IDs. Can be set to all for linking all resources.")

	rootCmd.PersistentFlags().BoolVarP(&useModernImportBlock, "modern-import-block", "", false, "Whether to generate HCL import blocks for generated resources instead of terraform import compatible CLI commands. This is only compatible with Terraform 1.5+")

	rootCmd.PersistentFlags().StringVar(&terraformInstallPath, "terraform-install-path", ".", "Path to an initialized Terraform working directory")

	if err = viper.BindPFlag("terraform-install-path", rootCmd.PersistentFlags().Lookup("terraform-install-path")); err != nil {
		log.Fatal(err)
	}

	rootCmd.PersistentFlags().StringVar(&terraformBinaryPath, "terraform-binary-path", "", "Path to an existing Terraform binary (otherwise, one will be downloaded)")

	if err = viper.BindPFlag("terraform-binary-path", rootCmd.PersistentFlags().Lookup("terraform-binary-path")); err != nil {
		log.Fatal(err)
	}

	if err = viper.BindEnv("terraform-install-path", "CLOUDFLARE_TERRAFORM_INSTALL_PATH"); err != nil {
		log.Fatal(err)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := homedir.Dir()
		if err != nil {
			log.Debug(err)
			return
		}

		// Search config in home directory with name ".dbtcloud-terraforming" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".dbtcloud-terraforming")
	}

	viper.AutomaticEnv() // read in environment variables that match
	viper.SetEnvPrefix("cf_terraforming")

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		log.Debug("using config file:", viper.ConfigFileUsed())
	}

	var cfgLogLevel = logrus.InfoLevel

	if verbose {
		cfgLogLevel = logrus.DebugLevel
	}

	log.SetLevel(cfgLogLevel)
}
