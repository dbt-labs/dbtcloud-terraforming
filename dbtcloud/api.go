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
	url := fmt.Sprintf("%s/v3/accounts/%s/credentials/", c.HostURL, c.AccountID)
	allCredentials := c.GetData(url)

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
