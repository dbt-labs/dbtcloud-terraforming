package dbtcloud

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/samber/lo"
)

type Response struct {
	Data []any `json:"data"`
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

func GetEndpoint(url string, config DbtCloudConfig) (error, []byte) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating a new request: %v", err)
	}
	// Add headers to the request
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.APIToken))

	// Create an HTTP client and make the request
	client := &http.Client{}
	resp, err := client.Do(req)
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

func GetData(config DbtCloudConfig, url string) []any {

	err, jsonPayload := GetEndpoint(url, config)

	var response Response

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data
}

func GetDataEnvVars(config DbtCloudConfig, url string) map[string]any {

	err, jsonPayload := GetEndpoint(url, config)

	var response EnvVarResponse

	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data.Variables
}

func GetProjects(config DbtCloudConfig) []any {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/projects/", config.Hostname, config.AccountID)

	return GetData(config, url)
}

func GetJobs(config DbtCloudConfig) []any {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/jobs/", config.Hostname, config.AccountID)

	return GetData(config, url)
}

func GetEnvironments(config DbtCloudConfig) []any {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/environments/", config.Hostname, config.AccountID)

	return GetData(config, url)
}

func GetRepositories(config DbtCloudConfig) []any {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/repositories/", config.Hostname, config.AccountID)

	return GetData(config, url)
}

func GetGroups(config DbtCloudConfig) []any {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/groups/", config.Hostname, config.AccountID)

	return GetData(config, url)
}

func GetEnvironmentVariables(config DbtCloudConfig, listProjects []int) map[int]any {

	allEnvVars := map[int]any{}

	projects := GetProjects(config)
	for _, project := range projects {
		projectTyped := project.(map[string]interface{})
		projectID := int(projectTyped["id"].(float64))

		if len(listProjects) > 0 && lo.Contains(listProjects, projectID) == false {
			continue
		}

		url := fmt.Sprintf("https://%s/api/v3/accounts/%s/projects/%d/environment-variables/environment/", config.Hostname, config.AccountID, projectID)
		allEnvVars[projectID] = GetDataEnvVars(config, url)
	}
	return allEnvVars
}

func GetCredentials(config DbtCloudConfig) []any {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/credentials/", config.Hostname, config.AccountID)

	// we need to remove all the credentials mapped to projects that are not active
	// those stay dangling in dbt Cloud

	allProjects := GetProjects(config)
	allProjectIDs := lo.Map(allProjects, func(project any, index int) int {
		return int(project.(map[string]interface{})["id"].(float64))
	})

	allCredentials := GetData(config, url)
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
