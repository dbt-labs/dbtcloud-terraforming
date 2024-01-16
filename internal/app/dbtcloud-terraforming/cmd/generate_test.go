package cmd

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/dbt-labs/dbtcloud-terraforming/dbtcloud"
	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/spf13/viper"

	"github.com/stretchr/testify/assert"
)

var (
	// listOfString is an example representation of a key where the value is a
	// list of string values.
	//
	//   resource "example" "example" {
	//     attr = [ "b", "c", "d"]
	//   }
	listOfString = []interface{}{"b", "c", "d"}

	// configBlockOfStrings is an example of where a key is a "block" assignment
	// in HCL.
	//
	//   resource "example" "example" {
	//     attr = {
	//       c = "d"
	//       e = "f"
	//     }
	//   }
	configBlockOfStrings = map[string]interface{}{
		"c": "d",
		"e": "f",
	}

	// TODO see if we want to keep this
	dbtCloudTestAccountID = "31"
)

func TestGenerate_writeAttrLine(t *testing.T) {
	multilineListOfStrings := heredoc.Doc(`
		a = ["b", "c", "d"]
	`)
	multilineBlock := heredoc.Doc(`
		a = {
		  c = "d"
		  e = "f"
		}
	`)
	tests := map[string]struct {
		key   string
		value interface{}
		want  string
	}{
		"value is string":           {key: "a", value: "b", want: fmt.Sprintf("a = %q\n", "b")},
		"value is int":              {key: "a", value: 1, want: "a = 1\n"},
		"value is float":            {key: "a", value: 1.0, want: "a = 1\n"},
		"value is bool":             {key: "a", value: true, want: "a = true\n"},
		"value is list of strings":  {key: "a", value: listOfString, want: multilineListOfStrings},
		"value is block of strings": {key: "a", value: configBlockOfStrings, want: multilineBlock},
		"value is nil":              {key: "a", value: nil, want: ""},
	}

	for name, tc := range tests {
		f := hclwrite.NewEmptyFile()
		t.Run(name, func(t *testing.T) {
			writeAttrLine(tc.key, tc.value, "", f.Body())
			assert.Equal(t, tc.want, string(f.Bytes()))
		})
	}
}

func TestGenerate_ResourceNotSupported(t *testing.T) {
	output, err := executeCommandC(rootCmd, "--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", "/Users/bper/dev/dbtcloud-terraforming", "generate", "--resource-types", "notreal")

	assert.Nil(t, err)
	assert.Equal(t, output, `"notreal" is not yet supported for automatic generation`)
}

func TestResourceGeneration(t *testing.T) {
	tests := map[string]struct {
		identifierType   string
		resourceType     string
		testdataFilename string
	}{
		"dbt Cloud projects":     {identifierType: "account", resourceType: "dbtcloud_project", testdataFilename: "dbtcloud_project"},
		"dbt Cloud jobs":         {identifierType: "account", resourceType: "dbtcloud_job", testdataFilename: "dbtcloud_job"},
		"dbt Cloud repositories": {identifierType: "account", resourceType: "dbtcloud_repository", testdataFilename: "dbtcloud_repository"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Reset the environment variables used in test to ensure we don't
			// have both present at once.

			var r *recorder.Recorder
			var err error
			if os.Getenv("OVERWRITE_VCR_CASSETTES") == "true" {
				r, err = recorder.NewWithOptions(&recorder.Options{CassetteName: "../../../../testdata/dbtcloud/" + tc.testdataFilename, Mode: recorder.ModeRecordOnly, RealTransport: http.DefaultTransport})
			} else {
				r, err = recorder.New("../../../../testdata/dbtcloud/" + tc.testdataFilename)
			}

			if err != nil {
				log.Fatal(err)
			}
			defer func() {
				err := r.Stop()
				if err != nil {
					log.Fatal(err)
				}
			}()

			r.AddHook(func(i *cassette.Interaction) error {
				// Sensitive HTTP headers
				delete(i.Request.Headers, "Authorization")

				// // HTTP response headers that we don't need to assert against
				delete(i.Response.Headers, "Access-Control-Allow-Headers")
				delete(i.Response.Headers, "Access-Control-Allow-Methods")
				delete(i.Response.Headers, "Access-Control-Allow-Origin")
				delete(i.Response.Headers, "Allow")
				delete(i.Response.Headers, "Referrer-Policy")
				delete(i.Response.Headers, "Connection")
				delete(i.Response.Headers, "Date")
				delete(i.Response.Headers, "Referrer-Policy")
				delete(i.Response.Headers, "Server")
				delete(i.Response.Headers, "Strict-Transport-Security")
				delete(i.Response.Headers, "Vary")
				delete(i.Response.Headers, "X-Content-Type-Options")
				delete(i.Response.Headers, "X-Frame-Options")
				delete(i.Response.Headers, "X-Request-Id")
				delete(i.Response.Headers, "X-Robots-Tag")

				reg := regexp.MustCompile(`ssh-rsa [^"]+`)
				i.Response.Body = reg.ReplaceAllString(i.Response.Body, "ssh-rsa --redacted--")

				return nil
			}, recorder.AfterCaptureHook)

			output := ""

			dbtCloudClient = dbtcloud.NewDbtCloudHTTPClient(viper.GetString("host-url"), viper.GetString("token"), viper.GetString("account"), r)

			// IMPORTANT!!! we need to reset the lists here otherwise subsequent tests will fail
			resourceTypes = []string{}
			listLinkedResources = []string{}

			output, err = executeCommandC(rootCmd, "--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", "/Users/bper/dev/dbtcloud-terraforming", "generate", "--resource-types", tc.resourceType, "--account", dbtCloudTestAccountID)
			if err != nil {
				log.Fatal(err)
			}

			expected := testDataFile(tc.testdataFilename)
			assert.Equal(t, strings.TrimRight(expected, "\n"), strings.TrimRight(output, "\n"))
		})
	}
}
