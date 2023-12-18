package dbtcloud

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/samber/lo"
)

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
	Hostname  string
	APIToken  string
	AccountID string
}

func NewDbtCloudHTTPClient(hostname, apiToken, accountID string) *DbtCloudHTTPClient {
	return &DbtCloudHTTPClient{
		Client:    &http.Client{},
		Hostname:  hostname,
		APIToken:  apiToken,
		AccountID: accountID,
	}
}

func (c *DbtCloudHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Add default headers to the request
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIToken))

	// Perform the request
	return c.Client.Do(req)
}

func (c *DbtCloudHTTPClient) GetEndpoint(url string) (error, []byte) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating a new request: %v", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		log.Fatalf("Error fetching URL %v: %v", url, err)
	}
	// Ensure the response body is closed at the end.
	defer resp.Body.Close()

	// Read the response body
	jsonPayload, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading body: %v", err)
	}
	return err, jsonPayload
}

func (c *DbtCloudHTTPClient) GetSingleData(url string) any {

	err, jsonPayload := c.GetEndpoint(url)
	var response SingleResponse

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data
}

func (c *DbtCloudHTTPClient) GetData(url string) []any {

	// get the first page
	err, jsonPayload := c.GetEndpoint(url)
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

		err, jsonPayload := c.GetEndpoint(newURL)
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

	err, jsonPayload := c.GetEndpoint(url)

	var response EnvVarResponse

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data.Variables
}

func (c *DbtCloudHTTPClient) GetProjects() []any {

	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/projects/", c.Hostname, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetJobs() []any {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/jobs/", c.Hostname, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetEnvironments() []any {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/environments/", c.Hostname, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetRepositories() []any {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/repositories/", c.Hostname, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetGroups() []any {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/groups/", c.Hostname, c.AccountID)

	return c.GetData(url)
}

func (c *DbtCloudHTTPClient) GetEnvironmentVariables(listProjects []int) map[int]any {

	allEnvVars := map[int]any{}

	projects := c.GetProjects()
	for _, project := range projects {
		projectTyped := project.(map[string]interface{})
		projectID := int(projectTyped["id"].(float64))

		if len(listProjects) > 0 && lo.Contains(listProjects, projectID) == false {
			continue
		}

		url := fmt.Sprintf("https://%s/api/v3/accounts/%s/projects/%d/environment-variables/environment/", c.Hostname, c.AccountID, projectID)
		allEnvVars[projectID] = c.GetDataEnvVars(url)
	}
	return allEnvVars
}

func (c *DbtCloudHTTPClient) GetConnections(listProjects []int, warehouses []string) []any {

	projects := c.GetProjects()
	connections := []any{}

	// we loop through all the projects to only get the active connections
	// there are dangling connections in dbt Cloud with state=1 that we don't want to import
	for _, project := range projects {
		projectTyped := project.(map[string]interface{})
		projectID := int(projectTyped["id"].(float64))

		if len(listProjects) > 0 && lo.Contains(listProjects, projectID) == false {
			continue
		}

		// we might have a project partially configured that we want to avoid
		if projectTyped["connection"] == nil {
			continue
		}

		projectConnectionTyped := projectTyped["connection"].(map[string]any)
		if !lo.Contains(warehouses, projectConnectionTyped["type"].(string)) {
			continue
		}

		url := fmt.Sprintf("https://%s/api/v3/accounts/%s/projects/%d/connections/%0.f/", c.Hostname, c.AccountID, projectID, projectConnectionTyped["id"].(float64))
		connection := c.GetSingleData(url)
		connections = append(connections, connection)
	}

	return connections
}

func (c *DbtCloudHTTPClient) GetBigQueryConnections(listProjects []int) []any {
	return c.GetConnections(listProjects, []string{"bigquery"})
}

func (c *DbtCloudHTTPClient) GetSnowflakeCredentials() []any {
	return c.GetWarehouseCredentials("snowflake")
}

func (c *DbtCloudHTTPClient) GetBigQueryCredentials() []any {
	return c.GetWarehouseCredentials("bigquery")
}

func (c *DbtCloudHTTPClient) GetWarehouseCredentials(warehouse string) []any {
	listCredentials := c.GetCredentials()
	warehouseCredentials := []any{}

	for _, credential := range listCredentials {
		credentialTyped := credential.(map[string]any)

		// we only import the relevant ones
		if credentialTyped["type"] != warehouse {
			continue
		}
		warehouseCredentials = append(warehouseCredentials, credential)
	}

	return warehouseCredentials
}

func (c *DbtCloudHTTPClient) GetCredentials() []any {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/credentials/", c.Hostname, c.AccountID)

	// we need to remove all the credentials mapped to projects that are not active
	// those stay dangling in dbt Cloud

	allProjects := c.GetProjects()
	allProjectIDs := lo.Map(allProjects, func(project any, index int) int {
		return int(project.(map[string]interface{})["id"].(float64))
	})

	allCredentials := c.GetData(url)
	validCredentials := []any{}

	for _, credential := range allCredentials {
		credentialTyped := credential.(map[string]interface{})
		credentialProjectID := int(credentialTyped["project_id"].(float64))

		if lo.Contains(allProjectIDs, credentialProjectID) == true {
			validCredentials = append(validCredentials, credential)
		}
	}
	return validCredentials
}
