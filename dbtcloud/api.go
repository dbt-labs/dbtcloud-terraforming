package dbtcloud

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type Response struct {
	Data []interface{} `json:"data"`
}

type DbtCloudConfig struct {
	Hostname  string
	APIToken  string
	AccountID string
}

func GetEndpoint(config DbtCloudConfig, url string) []interface{} {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating a new request: %v", err)
	}

	// Add headers to the request
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.APIToken))
	// req.Header.Set("Custom-Header", "Custom-Value")

	// Create an HTTP client and make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error fetching URL %v: %v", url, err)
	}
	defer resp.Body.Close() // Ensure the response body is closed at the end.

	// Read the response body
	jsonPayload, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading body: %v", err)
	}

	var response Response

	// var jsonStructData []interface{}
	err = json.Unmarshal(jsonPayload, &response)
	if err != nil {
		log.Fatal(err)
	}

	return response.Data
}

func GetProjects(config DbtCloudConfig) []interface{} {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/projects/", config.Hostname, config.AccountID)

	return GetEndpoint(config, url)
}

func GetJobs(config DbtCloudConfig) []interface{} {
	url := fmt.Sprintf("https://%s/api/v2/accounts/%s/jobs/", config.Hostname, config.AccountID)

	return GetEndpoint(config, url)
}

func GetEnvironments(config DbtCloudConfig) []interface{} {
	url := fmt.Sprintf("https://%s/api/v3/accounts/%s/environments/", config.Hostname, config.AccountID)

	return GetEndpoint(config, url)
}
