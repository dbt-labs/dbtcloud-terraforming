package dbtcloud

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

var versionString = "dev"

type Response struct {
	Data  []any `json:"data"`
	Extra Extra `json:"extra"`
}

type SingleResponse struct {
	Data any `json:"data"`
}

type Extra struct {
	Pagination Pagination `json:"pagination"`
}

type Pagination struct {
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

type EnvVarResponse struct {
	Data EnvVarData `json:"data"`
}

type EnvVarData struct {
	Environments []any          `json:"environments"`
	Variables    map[string]any `json:"variables"`
}

type DbtCloudConfig struct {
	Hostname  string
	APIToken  string
	AccountID string
}

type DbtCloudHTTPClient struct {
	Client    *http.Client
	HostURL   string
	APIToken  string
	AccountID string
}

type RateLimitedTransport struct {
	*http.Transport
	limiter *rate.Limiter
}

var log = logrus.New()

// RoundTrip overrides the http.RoundTrip to implement rate limiting.
func (t *RateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Wait for permission from the rate limiter
	t.limiter.Wait(req.Context())

	// Proceed with the request
	return t.Transport.RoundTrip(req)
}

func NewDbtCloudHTTPClient(hostURL, apiToken, accountID string, transport http.RoundTripper) *DbtCloudHTTPClient {
	if transport == nil {

		limiter := rate.NewLimiter(rate.Every(time.Minute), 3000)

		// Create a custom transport which is a modified clone of DefaultTransport
		// DefaultTransport handles https_proxy env var that we need to capture HTTP calls
		defaultTransport := http.DefaultTransport.(*http.Transport).Clone()
		transport = &RateLimitedTransport{
			Transport: defaultTransport,
			limiter:   limiter,
		}
	}
	return &DbtCloudHTTPClient{
		Client:    &http.Client{Transport: transport},
		HostURL:   hostURL,
		APIToken:  apiToken,
		AccountID: accountID,
	}
}

func (c *DbtCloudHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Add default headers to the request
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIToken))

	userAgentWithVersion := fmt.Sprintf(
		"dbtcloud-terraforming/%s",
		versionString,
	)
	req.Header.Set("User-Agent", userAgentWithVersion)

	// Perform the request
	return c.Client.Do(req)
}

func (c *DbtCloudHTTPClient) GetEndpoint(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating a new request: %v", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching URL %v: %v", url, err)
	}
	// Ensure the response body is closed at the end.
	defer resp.Body.Close()

	// Read the response body
	jsonPayload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body: %v", err)
	}

	// 400 and more are errors, either on the client side or the server side
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("error fetching URL %v: %v -- body: %s", url, resp.Status, string(jsonPayload))
	}

	return jsonPayload, nil
}

func (c *DbtCloudHTTPClient) GetSingleData(url string) (any, error) {

	jsonPayload, err := c.GetEndpoint(url)
	if err != nil {
		return nil, err
	}
	var response SingleResponse

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		return nil, err
	}

	return response.Data, nil
}

func (c *DbtCloudHTTPClient) GetData(url string) []any {

	// get the first page
	jsonPayload, err := c.GetEndpoint(url)
	if err != nil {
		log.Fatal(err)
	}

	var response Response

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	allResponses := response.Data

	count := response.Extra.Pagination.Count
	for count < response.Extra.Pagination.TotalCount {
		// get the next page

		var newURL string
		lastPartURL, _ := lo.Last(strings.Split(url, "/"))
		if strings.Contains(lastPartURL, "?") {
			newURL = fmt.Sprintf("%s&offset=%d", url, count)
		} else {
			newURL = fmt.Sprintf("%s?offset=%d", url, count)
		}

		jsonPayload, err := c.GetEndpoint(newURL)
		if err != nil {
			log.Fatal(err)
		}
		var response Response

		err = json.Unmarshal(jsonPayload, &response)
		if err != nil {
			log.Fatal(err)
		}

		if response.Extra.Pagination.Count == 0 {
			// Unlucky! one object might have been deleted since the first call
			// if we don't stop here we will loop forever!
			break
		} else {
			count += response.Extra.Pagination.Count
		}
		allResponses = append(allResponses, response.Data...)
	}

	return allResponses
}

func (c *DbtCloudHTTPClient) GetDataEnvVars(url string) map[string]any {

	jsonPayload, err := c.GetEndpoint(url)
	if err != nil {
		log.Fatal(err)
	}

	var response EnvVarResponse

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data.Variables
}

func (c *DbtCloudHTTPClient) GetProjects(listProjects []int) []any {
	url := fmt.Sprintf("%s/v2/accounts/%s/projects/", c.HostURL, c.AccountID)
	allProjects := c.GetData(url)

	if len(listProjects) == 0 {
		return allProjects
	}

	filteredProjects := []any{}

	for _, data := range allProjects {
		dataTyped := data.(map[string]any)
		projectID := dataTyped["id"].(float64)

		if len(listProjects) > 0 && !lo.Contains(listProjects, int(projectID)) {
			continue
		}
		filteredProjects = append(filteredProjects, data)
	}

	return filteredProjects
}

func (c *DbtCloudHTTPClient) GetJobs(listProjects []int) []any {
	url := fmt.Sprintf("%s/v2/accounts/%s/jobs/", c.HostURL, c.AccountID)
	allJobs := c.GetData(url)
	filteredJobs := filterByProject(allJobs, listProjects)

	return filteredJobs
}

func filterByProject(allData []any, listProjects []int) []any {

	// if there is no filter provided we return the data as is
	if len(listProjects) == 0 {
		return allData
	}

	filteredData := []any{}
	for _, data := range allData {
		dataTyped := data.(map[string]any)
		projectID := dataTyped["project_id"].(float64)

		if !lo.Contains(listProjects, int(projectID)) {
			continue
		}
		filteredData = append(filteredData, data)
	}
	return filteredData
}

func (c *DbtCloudHTTPClient) GetEnvironments(listProjects []int) []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/environments/", c.HostURL, c.AccountID)
	allEnvironments := c.GetData(url)
	filteredEnvironments := filterByProject(allEnvironments, listProjects)

	return filteredEnvironments
}

func (c *DbtCloudHTTPClient) GetRepositories(listProjects []int) []any {
	url := fmt.Sprintf("%s/v2/accounts/%s/repositories/", c.HostURL, c.AccountID)
	allRepos := c.GetData(url)
	filteredRepos := filterByProject(allRepos, listProjects)

	return filteredRepos
}

func (c *DbtCloudHTTPClient) GetGroups() []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/groups/", c.HostURL, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetEnvironmentVariables(listProjects []int) map[int]any {

	allEnvVars := map[int]any{}

	projects := c.GetProjects(listProjects)
	for _, project := range projects {
		projectTyped := project.(map[string]any)
		projectID := int(projectTyped["id"].(float64))

		if len(listProjects) > 0 && !lo.Contains(listProjects, projectID) {
			continue
		}

		url := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/environment-variables/environment/", c.HostURL, c.AccountID, projectID)
		allEnvVars[projectID] = c.GetDataEnvVars(url)
	}
	return allEnvVars
}

func (c *DbtCloudHTTPClient) GetConnections(listProjects []int, warehouses []string) []any {

	projects := c.GetProjects(listProjects)
	connections := []any{}

	// we loop through all the projects to only get the active connections
	// there are dangling connections in dbt Cloud with state=1 that we don't want to import
	for _, project := range projects {
		projectTyped := project.(map[string]any)
		projectID := int(projectTyped["id"].(float64))

		if len(listProjects) > 0 && !lo.Contains(listProjects, projectID) {
			continue
		}

		// we might have a project partially configured that we want to avoid
		if projectTyped["connection"] == nil {
			continue
		}

		projectConnectionTyped := projectTyped["connection"].(map[string]any)
		connectionType := projectConnectionTyped["type"].(string)
		if connectionType == "adapter" {
			// this is very ugly but we need to traverse down...
			detailsTyped := projectConnectionTyped["details"].(map[string]any)
			connectionDetailsTyped := detailsTyped["connection_details"].(map[string]any)
			fieldsTyped := connectionDetailsTyped["fields"].(map[string]any)
			typeTyped := fieldsTyped["type"].(map[string]any)
			connectionType = fmt.Sprintf("adapter/%s", typeTyped["value"].(string))
		}
		if !lo.Contains(warehouses, connectionType) {
			continue
		}

		url := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/connections/%0.f/", c.HostURL, c.AccountID, projectID, projectConnectionTyped["id"].(float64))
		connection, err := c.GetSingleData(url)
		if err != nil {
			log.Warn(err)
			continue
		}

		connections = append(connections, connection)
	}

	return connections
}

func (c *DbtCloudHTTPClient) GetGenericConnections(listProjects []int) []any {
	return c.GetConnections(listProjects, []string{"snowflake", "postgres", "redshift", "adapter/spark", "adapter/databricks"})
}

func (c *DbtCloudHTTPClient) GetBigQueryConnections(listProjects []int) []any {
	return c.GetConnections(listProjects, []string{"bigquery"})
}

func (c *DbtCloudHTTPClient) GetFabricConnections(listProjects []int) []any {
	return c.GetConnections(listProjects, []string{"adapter/fabric"})
}

func (c *DbtCloudHTTPClient) GetSnowflakeCredentials(listProjects []int) []any {
	return c.GetWarehouseCredentials(listProjects, "snowflake")
}

func (c *DbtCloudHTTPClient) GetDatabricksCredentials(listProjects []int) []any {
	return c.GetWarehouseCredentials(listProjects, "databricks")
}

func (c *DbtCloudHTTPClient) GetBigQueryCredentials(listProjects []int) []any {
	return c.GetWarehouseCredentials(listProjects, "bigquery")
}

func (c *DbtCloudHTTPClient) GetWarehouseCredentials(listProjects []int, warehouse string) []any {
	listCredentials := c.GetCredentials(listProjects)
	warehouseCredentials := []any{}

	listCredentialIDs := []int{}

	for _, credential := range listCredentials {
		credentialTyped := credential.(map[string]any)

		// we only import the relevant ones
		if credentialTyped["type"] != "adapter" && credentialTyped["type"] != warehouse {
			continue
		}

		if credentialTyped["type"] == "adapter" && credentialTyped["adapter_version"] != fmt.Sprintf("%s_v0", warehouse) {
			continue
		}

		// the API has some issues with offsets and we can get duplicates
		if lo.Contains(listCredentialIDs, int(credentialTyped["id"].(float64))) {
			continue
		}

		warehouseCredentials = append(warehouseCredentials, credential)
		listCredentialIDs = append(listCredentialIDs, int(credentialTyped["id"].(float64)))
	}

	return warehouseCredentials
}

func (c *DbtCloudHTTPClient) GetCredentials(listProjects []int) []any {
	allCredentials := []any{}
	for _, projectID := range listProjects {
		url := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/credentials/", c.HostURL, c.AccountID, projectID)
		projectCredentials := c.GetData(url)
		allCredentials = append(allCredentials, projectCredentials...)
	}

	// we need to keep only the credentials for active environments
	allEnvironments := c.GetEnvironments(listProjects)

	credentialToEnvironmentID := map[float64]float64{}
	lo.ForEach(allEnvironments, func(env any, index int) {
		envTyped := env.(map[string]any)
		credentialID, ok := envTyped["credentials_id"].(float64)
		if ok {
			environmentID := envTyped["id"].(float64)
			credentialToEnvironmentID[credentialID] = environmentID
		}
	})

	filteredCredentials := []any{}
	for _, credential := range allCredentials {
		dataTyped := credential.(map[string]any)
		credentialsID := dataTyped["id"].(float64)
		if _, ok := credentialToEnvironmentID[credentialsID]; !ok {
			continue
		}
		credential.(map[string]any)["environment_id"] = credentialToEnvironmentID[credentialsID]
		filteredCredentials = append(filteredCredentials, credential)
	}
	return filteredCredentials
}

func (c *DbtCloudHTTPClient) GetExtendedAttributes(listProjects []int) []any {

	allExtendedAttributes := []any{}
	envs := c.GetEnvironments(listProjects)
	for _, env := range envs {
		envTyped := env.(map[string]any)
		extendedAttributesID, err := envTyped["extended_attributes_id"].(float64)
		if !err {
			continue
		}
		projectID := envTyped["project_id"].(float64)
		url := fmt.Sprintf("%s/v3/accounts/%s/projects/%0.f/extended-attributes/%0.f/", c.HostURL, c.AccountID, projectID, extendedAttributesID)
		extendedAttributes, _ := c.GetSingleData(url)
		allExtendedAttributes = append(allExtendedAttributes, extendedAttributes)
	}
	return allExtendedAttributes
}

func (c *DbtCloudHTTPClient) GetUsers() []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/users/", c.HostURL, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetWebhooks() []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/webhooks/subscriptions", c.HostURL, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetNotifications() []any {
	url := fmt.Sprintf("%s/v2/accounts/%s/notifications/", c.HostURL, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetServiceTokens() []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/service-tokens/", c.HostURL, c.AccountID)

	// the API returns the deactivated ones as well :-(
	allServiceTokens := c.GetData(url)

	activeServiceTokens := lo.Filter(allServiceTokens, func(serviceToken any, idx int) bool {
		serviceTokenTyped := serviceToken.(map[string]any)
		return serviceTokenTyped["state"].(float64) == 1
	})
	return activeServiceTokens
}

func (c *DbtCloudHTTPClient) GetServiceTokenPermissions(serviceTokenID int) []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/service-tokens/%d/permissions/", c.HostURL, c.AccountID, serviceTokenID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetGlobalConnection(id int64) (any, error) {
	url := fmt.Sprintf("%s/v3/accounts/%s/connections/%d/", c.HostURL, c.AccountID, id)

	return c.GetSingleData(url)
}

func (c *DbtCloudHTTPClient) GetCredential(projectId, id int64) (any, error) {
	url := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/credentials/%d/", c.HostURL, c.AccountID, projectId, id)

	return c.GetSingleData(url)
}

func (c *DbtCloudHTTPClient) GetGlobalConnectionsSummary() []any {
	url := fmt.Sprintf("%s/v3/accounts/%s/connections/", c.HostURL, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetGlobalConnections() []any {

	// this return just a summary though...
	// so we need to loop through the results to get the details
	allConnectionsSummary := c.GetGlobalConnectionsSummary()
	allConnectionDetails := []any{}

	for _, connectionSummary := range allConnectionsSummary {
		connectionSummaryTyped := connectionSummary.(map[string]any)
		connectionID := int(connectionSummaryTyped["id"].(float64))
		connectionDetails, _ := c.GetGlobalConnection(int64(connectionID))
		allConnectionDetails = append(allConnectionDetails, connectionDetails)
	}

	return allConnectionDetails
}

// EnvVarJobOverrideResponse mirrors the response envelope returned by the
// job-scoped environment-variable override endpoint
// (/v3/accounts/{account}/projects/{project}/environment-variables/job/?job_definition_id={job}).
// Unlike the environment-scoped endpoint (see EnvVarResponse/EnvVarData
// above), this endpoint's "data" is a flat map of env var name -> per-scope
// override details (e.g. {"account": {...}, "environment": {...}, "job":
// {...}}), with no intermediate "variables" wrapper.
type EnvVarJobOverrideResponse struct {
	Data map[string]any `json:"data"`
}

func (c *DbtCloudHTTPClient) GetDataEnvVarJobOverrides(url string) map[string]any {

	jsonPayload, err := c.GetEndpoint(url)
	if err != nil {
		log.Fatal(err)
	}

	var response EnvVarJobOverrideResponse

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data
}

// GetEnvironmentVariableJobOverrides fetches per-job environment-variable
// value overrides for the given jobs (typically the already-prefetched jobs
// list generate.go/import.go build for other job-scoped resources), scoped
// to listProjects.
//
// The override data doesn't live behind an account- or project-wide "list
// all overrides" endpoint - it's only queryable per job_definition_id (see
// GetEnvironmentVariableJobOverride in the provider's own client, which takes
// a job_definition_id query param) - so we loop through the known jobs for
// each project and query per job, mirroring the per-project-then-per-item
// fetch pattern GetProfiles already uses above.
//
// Each returned item is a flattened map with "name" (the env var name),
// "project_id", "job_definition_id", "environment_variable_job_override_id"
// (the override's own numeric id - present so callers can fold a
// project/job/override composite into a unique resource id, exactly as
// GetProfiles's callers do for profile_id), and "raw_value" (the override's
// value, matching the dbtcloud_environment_variable_job_override resource's
// own attribute name).
func (c *DbtCloudHTTPClient) GetEnvironmentVariableJobOverrides(listProjects []int, jobs []any) []any {
	allOverrides := []any{}

	jobIDsByProject := map[int][]int{}
	for _, job := range jobs {
		jobTyped := job.(map[string]any)
		jobID := int(jobTyped["id"].(float64))
		projectID := int(jobTyped["project_id"].(float64))
		jobIDsByProject[projectID] = append(jobIDsByProject[projectID], jobID)
	}

	for projectID, jobIDs := range jobIDsByProject {
		if len(listProjects) > 0 && !lo.Contains(listProjects, projectID) {
			continue
		}

		for _, jobID := range jobIDs {
			url := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/environment-variables/job/?job_definition_id=%d", c.HostURL, c.AccountID, projectID, jobID)
			jobOverrides := c.GetDataEnvVarJobOverrides(url)

			for envVarName, value := range jobOverrides {
				// "project" is a pseudo-key returned alongside the per-variable
				// entries on the environment-scoped endpoint (see
				// GetEnvironmentVariables); we haven't observed it here but skip
				// it defensively for the same reason.
				if envVarName == "project" {
					continue
				}

				valueTyped, ok := value.(map[string]any)
				if !ok {
					continue
				}

				jobOverrideTyped, ok := valueTyped["job"].(map[string]any)
				if !ok || jobOverrideTyped == nil {
					// this env var has no job-level override for this job - only
					// account/environment-level values, which aren't this
					// resource's concern.
					continue
				}

				overrideValue, _ := jobOverrideTyped["value"].(string)

				allOverrides = append(allOverrides, map[string]any{
					"name":                                 envVarName,
					"project_id":                           float64(projectID),
					"job_definition_id":                    float64(jobID),
					"environment_variable_job_override_id": jobOverrideTyped["id"],
					"raw_value":                            overrideValue,
				})
			}
		}
	}

	return allOverrides
}

// accountFeaturesData mirrors the payload returned by the account features
// endpoint. The JSON keys on the wire use a mix of hyphens and underscores;
// they get normalized to the underscored attribute names used by the
// `dbtcloud_account_features` Terraform resource schema when GetAccountFeatures
// builds its result map below.
type accountFeaturesData struct {
	AdvancedCI                 bool `json:"advanced-ci"`
	PartialParsing             bool `json:"partial-parsing"`
	RepoCaching                bool `json:"repo-caching"`
	AIFeatures                 bool `json:"ai_features"`
	CatalogIngestion           bool `json:"catalog-ingestion"`
	ExplorerAccountUI          bool `json:"explorer-account-ui"`
	FusionMigrationPermissions bool `json:"fusion-migration-permissions"`
}

type accountFeaturesResponse struct {
	Data accountFeaturesData `json:"data"`
}

// GetAccountFeatures fetches the account-level feature flags (Advanced CI, AI
// features, catalog ingestion, etc.) that gate whether certain dbt Cloud
// resources (e.g. jobs using Advanced CI) can be created in an account.
//
// Unlike every other resource in this file, account features are a singleton:
// there is exactly one set of flags per account rather than a list of items,
// and the object has no numeric `id` of its own. We return a single-element
// slice with `id` set to the account id so it fits the same []any shape the
// rest of the generator/importer code already works with.
func (c *DbtCloudHTTPClient) GetAccountFeatures() []any {
	url := fmt.Sprintf("%s/private/accounts/%s/features/", c.HostURL, c.AccountID)

	jsonPayload, err := c.GetEndpoint(url)
	if err != nil {
		log.Fatal(err)
	}

	var response accountFeaturesResponse
	if err := json.Unmarshal(jsonPayload, &response); err != nil {
		log.Fatal(err)
	}

	features := map[string]any{
		"id":                           c.AccountID,
		"advanced_ci":                  response.Data.AdvancedCI,
		"partial_parsing":              response.Data.PartialParsing,
		"repo_caching":                 response.Data.RepoCaching,
		"ai_features":                  response.Data.AIFeatures,
		"catalog_ingestion":            response.Data.CatalogIngestion,
		"explorer_account_ui":          response.Data.ExplorerAccountUI,
		"fusion_migration_permissions": response.Data.FusionMigrationPermissions,
	}

	return []any{features}
}

// GetProfiles fetches the profiles (project-scoped bindings of a connection,
// credentials, and optional extended attributes) for each project in
// listProjects, mirroring the per-project fetch pattern already used by
// GetConnections/GetEnvironmentVariables above.
//
// The profiles endpoint itself doesn't return the warehouse type of the
// credentials a profile points at, but callers (see the dbtcloud_profile
// case in generate.go) need that to know which specific credential resource
// type (dbtcloud_snowflake_credential, dbtcloud_bigquery_credential, etc.) a
// profile's credentials_id should be linked to. We can't reuse
// GetCredentials for this lookup because it only returns credentials that
// are still attached to an environment's legacy credentials_id - which a
// profile-bound environment's credentials never are, since those are bound
// through the profile instead. So we fetch each project's credentials list
// directly here and attach the matching credential (under the "credentials"
// key) to each profile, mirroring how the environments endpoint already
// embeds a nested "credentials" object for the same purpose.
func (c *DbtCloudHTTPClient) GetProfiles(listProjects []int) []any {
	projects := c.GetProjects(listProjects)
	allProfiles := []any{}

	for _, project := range projects {
		projectTyped := project.(map[string]any)
		projectID := int(projectTyped["id"].(float64))

		if len(listProjects) > 0 && !lo.Contains(listProjects, projectID) {
			continue
		}

		url := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/profiles/", c.HostURL, c.AccountID, projectID)
		projectProfiles := c.GetData(url)

		if len(projectProfiles) == 0 {
			continue
		}

		credentialsURL := fmt.Sprintf("%s/v3/accounts/%s/projects/%d/credentials/", c.HostURL, c.AccountID, projectID)
		projectCredentials := c.GetData(credentialsURL)
		credentialByID := map[float64]map[string]any{}
		for _, credential := range projectCredentials {
			credentialTyped := credential.(map[string]any)
			credentialByID[credentialTyped["id"].(float64)] = credentialTyped
		}

		for _, profile := range projectProfiles {
			profileTyped := profile.(map[string]any)
			if credentialsID, ok := profileTyped["credentials_id"].(float64); ok {
				if credentialDetails, found := credentialByID[credentialsID]; found {
					profileTyped["credentials"] = credentialDetails
				}
			}
			allProfiles = append(allProfiles, profileTyped)
		}
	}

	return allProfiles
}
