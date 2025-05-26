package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/dbt-labs/dbtcloud-terraforming/dbtcloud"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(interactiveCmd)
}

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Interactive mode to configure and run dbtcloud-terraforming",
	Long:  `Walks through configuration options interactively using a terminal UI`,
	Run:   runInteractive,
}

func runInteractive(cmd *cobra.Command, args []string) {
	// Initialize variables to store user input
	var selectedCommand string
	var selectedResources []string
	var selectedLinkedResources []string
	var useModernImport bool
	var selectedProjects []int

	// First group - Credentials (only if not already set)
	var credentialFields []huh.Field
	if viper.GetString("account") == "" {
		credentialFields = append(credentialFields,
			huh.NewInput().
				Title("Account ID").
				Value(&accountID))
	}
	if viper.GetString("token") == "" {
		credentialFields = append(credentialFields,
			huh.NewInput().
				Title("API Token").
				Value(&apiToken).
				EchoMode(huh.EchoModePassword))
	}
	if viper.GetString("host-url") == "" {
		credentialFields = append(credentialFields,
			huh.NewInput().
				Title("Host URL (optional, default is https://cloud.getdbt.com/api)").
				Value(&hostURL))
	}

	// Only show credentials form if we have fields to show
	if len(credentialFields) > 0 {
		credentialsForm := huh.NewForm(
			huh.NewGroup(credentialFields...),
		).WithTheme(huh.ThemeCatppuccin())

		if err := credentialsForm.Run(); err != nil {
			log.Fatal(err)
		}

		// Update credentials in viper
		if accountID != "" {
			viper.Set("account", accountID)
		}
		if apiToken != "" {
			viper.Set("token", apiToken)
		}
		if hostURL != "" {
			viper.Set("host-url", hostURL)
		}
	}

	// Initialize the client after getting credentials
	if err := initializeClient(); err != nil {
		log.Fatal(err)
	}

	// Get all available resource types
	availableResources := make([]string, 0, len(resourceImportStringFormats))
	for resource := range resourceImportStringFormats {
		availableResources = append(availableResources, resource)
	}
	sort.Strings(availableResources)

	// Build the main form groups
	var groups []*huh.Group

	// Get available projects and add project selection
	projects := dbtCloudClient.GetProjects([]int{})
	projectOptions := make([]huh.Option[int], 0, len(projects))
	for _, p := range projects {
		project := p.(map[string]interface{})
		projectOptions = append(projectOptions,
			huh.NewOption(
				fmt.Sprintf("%v - %s", int(project["id"].(float64)), project["name"].(string)),
				int(project["id"].(float64)),
			),
		)
	}

	if len(projectOptions) > 0 {
		groups = append(groups, huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Select Projects to Include (optional) - Use x or space to select/deselect").
				Options(projectOptions...).
				Value(&selectedProjects),
		))
	}

	// group 2 - Command and resource selection
	groups = append(groups, huh.NewGroup(
		// Command selection
		huh.NewSelect[string]().
			Title("Select Command").
			Options(
				huh.NewOption("Generate both resource configurations and import commands/blocks", "genimport"),
				huh.NewOption("Generate resources configuration", "generate"),
				huh.NewOption("Generate import commands/blocks", "import"),
			).
			Value(&selectedCommand),

		// Resource types multiselect
		huh.NewMultiSelect[string]().
			Title("Select Resource Types - Use x or space to select/deselect - At least one is required").
			Options(func() []huh.Option[string] {
				opts := make([]huh.Option[string], 0, len(availableResources)+1)
				opts = append(opts, huh.NewOption("All resources", "all"))
				for _, r := range availableResources {
					opts = append(opts, huh.NewOption(r, r))
				}
				return opts
			}()...).
			Value(&selectedResources).
			Validate(func(selected []string) error {
				if len(selected) == 0 {
					return errors.New("you must select at least one item")
				}
				return nil
			}),

		// Exclude resource types multiselect
		huh.NewMultiSelect[string]().
			Title("Select Resource Types to Exclude - Use x or space to select/deselect - Likely to be used with --resource-types all").
			Options(func() []huh.Option[string] {
				opts := make([]huh.Option[string], 0, len(availableResources))
				for _, r := range availableResources {
					opts = append(opts, huh.NewOption(r, r))
				}
				return opts
			}()...).
			Value(&excludeResourceTypes),

		// Linked resources multiselect
		huh.NewMultiSelect[string]().
			Title("Select Resources to Link (dependencies) - Use x or space to select/deselect").
			Options(func() []huh.Option[string] {
				opts := make([]huh.Option[string], 0, len(availableResources)+1)
				opts = append(opts, huh.NewOption("All resources", "all"))
				for _, r := range availableResources {
					opts = append(opts, huh.NewOption(r, r))
				}
				return opts
			}()...).
			Value(&selectedLinkedResources),
	))

	// group 3 - Generate options
	if !rootCmd.PersistentFlags().Changed("parameterize-jobs") {
		parameterizeJobs = false
		groups = append(groups, huh.NewGroup(
			// Parameterize jobs option
			huh.NewConfirm().
				Title("Parameterize jobs? (Creates locals to control job triggers)").
				Value(&parameterizeJobs),
		).WithHideFunc(func() bool {
			// Only show the parameterize jobs option if the command is not generate
			return selectedCommand == "import"
		}))
	}

	//  group 4 - Import options
	if !rootCmd.PersistentFlags().Changed("modern-import-block") {
		useModernImport = true
		groups = append(groups, huh.NewGroup(
			// Modern import block option
			huh.NewConfirm().
				Title("Use modern import blocks? (Terraform 1.5+ required)").
				Value(&useModernImport),
		).WithHideFunc(func() bool {
			// Only show the modern import block option if the command is not generate
			return selectedCommand == "generate"
		}))
	}

	// group 5 - Output options
	groups = append(groups, huh.NewGroup(
		huh.NewInput().
			Title("Output file path, should end with .tf (optional, will show output in stdout if not set)").
			Value(&outputFile),
	))

	// Create and run the main form
	mainForm := huh.NewForm(groups...).WithTheme(huh.ThemeCatppuccin())

	// Run the form
	if err := mainForm.Run(); err != nil {
		log.Fatal(err)
	}

	// Handle resource types selection
	if len(selectedResources) > 0 {
		if selectedResources[0] == "all" {
			resourceTypes = []string{"all"}
		} else {
			resourceTypes = selectedResources
		}
	}

	// Handle linked resources selection
	if len(selectedLinkedResources) > 0 {
		if selectedLinkedResources[0] == "all" {
			listLinkedResources = []string{"all"}
		} else {
			listLinkedResources = selectedLinkedResources
		}
	}

	// Update listFilterProjects with selection
	if len(selectedProjects) > 0 {
		listFilterProjects = selectedProjects
	}

	useModernImportBlock = useModernImport

	// Execute the selected command
	var cmdToRun *cobra.Command
	switch selectedCommand {
	case "generate":
		cmdToRun = generateCmd
	case "import":
		cmdToRun = importCommand
	case "genimport":
		cmdToRun = genimportCmd
	default:
		log.Fatal("Invalid command selected")
	}

	// Build args string for logging
	args = []string{
		"--resource-types", strings.Join(resourceTypes, ","),
		"--linked-resource-types", strings.Join(listLinkedResources, ","),
		"--exclude-resource-types", strings.Join(excludeResourceTypes, ","),
	}
	if useModernImportBlock && selectedCommand != "generate" {
		args = append(args, "--modern-import-block")
	}
	if outputFile != "" {
		args = append(args, "--output", outputFile)
	}
	if len(listFilterProjects) > 0 {
		args = append(args, "--projects", strings.Join(lo.Map(listFilterProjects, func(p int, _ int) string {
			return fmt.Sprintf("%d", p)
		}), ","))
	}

	cmd.Printf("\n# Executing: dbtcloud-terraforming %s %s\n\n", selectedCommand, strings.Join(args, " "))
	cmdToRun.Run(cmd, args)
}

// Helper function to initialize client
func initializeClient() error {
	account := viper.GetString("account")
	token := viper.GetString("token")
	hostURL := viper.GetString("host-url")

	if account == "" {
		return fmt.Errorf("account ID is required")
	}
	if token == "" {
		return fmt.Errorf("API token is required")
	}

	// Initialize the client
	dbtCloudClient = dbtcloud.NewDbtCloudHTTPClient(hostURL, token, account, nil)

	return nil
}
