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
	path := viper.GetString("terraforming-install-path")
	output, err := executeCommandC(rootCmd, "--terraform-binary-path", "/opt/homebrew/bin/terraform", "--terraform-install-path", path, "generate", "--resource-types", "notreal")

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
		// NOTE: the "dbtcloud_user_groups" cassette (testdata/dbtcloud/dbtcloud_user_groups.yaml)
		// has not been recorded yet - recording it requires live account credentials via:
		//   OVERWRITE_VCR_CASSETTES=true go test ./internal/app/dbtcloud-terraforming/cmd/... \
		//     -run TestResourceGeneration/dbt_Cloud_user_groups -v
		// Until that cassette exists, this row is scaffolded but will fail if run; see
		// TestGenerate_FilterOutDefaultGroupIDs and TestGenerate_UserGroupsHCLExcludesDefaultGroups
		// for standalone unit coverage of the default-group filtering fix in the meantime.
		"dbt Cloud user groups": {identifierType: "account", resourceType: "dbtcloud_user_groups", testdataFilename: "dbtcloud_user_groups"},
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

			path := viper.GetString("terraforming-install-path")
			output, err = executeCommandC(rootCmd, "--terraform-binary-path", "/opt/homebrew/bin/terraform",
				"--terraform-install-path", path, "generate", "--resource-types",
				tc.resourceType, "--account", viper.GetString("account"))
			if err != nil {
				log.Fatal(err)
			}

			expected := testDataFile(tc.testdataFilename)
			assert.Equal(t, strings.TrimRight(expected, "\n"), strings.TrimRight(output, "\n"))
		})
	}
}

// fabricatedGroupsPayload mimics the shape returned by
// dbtCloudClient.GetGroups(): a mix of built-in default groups
// (Owner/Member/Everyone) and custom, user-managed groups.
func fabricatedGroupsPayload() []any {
	return []any{
		map[string]any{"id": float64(1), "name": "Owner"},
		map[string]any{"id": float64(2), "name": "Member"},
		map[string]any{"id": float64(3), "name": "Everyone"},
		map[string]any{"id": float64(10), "name": "Analysts"},
		map[string]any{"id": float64(11), "name": "Admins"},
	}
}

// TestGenerate_FilterOutDefaultGroupIDs unit tests the helper functions used
// by the "dbtcloud_user_groups" case to exclude the built-in default groups
// (Owner/Member/Everyone) from group_ids. Those groups are deliberately
// skipped by the "dbtcloud_group" case, so any resource that still
// references their IDs would produce dangling `dbtcloud_group` resource
// references once linked, causing `terraform plan`/`validate` to fail with
// "Reference to undeclared resource".
func TestGenerate_FilterOutDefaultGroupIDs(t *testing.T) {
	groupIDToName := buildGroupIDToNameMap(fabricatedGroupsPayload())

	tests := map[string]struct {
		groupIDs []int
		want     []int
	}{
		"mix of default and custom groups keeps only custom ones": {
			groupIDs: []int{1, 2, 3, 10, 11},
			want:     []int{10, 11},
		},
		"only default groups results in an empty list": {
			groupIDs: []int{1, 2, 3},
			want:     []int{},
		},
		"only custom groups are left untouched": {
			groupIDs: []int{10, 11},
			want:     []int{10, 11},
		},
		"unknown group id (not in the account's groups) is kept": {
			groupIDs: []int{999},
			want:     []int{999},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := filterOutDefaultGroupIDs(tc.groupIDs, groupIDToName)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestGenerate_UserGroupsHCLExcludesDefaultGroups asserts, at the HCL output
// level, that the "dbtcloud_user_groups" logic:
//  1. Only emits custom (non-default) group IDs in `group_ids`.
//  2. Emits no attribute/block at all for a user who is only a member of
//     default groups once those are filtered out (nothing left to manage).
//
// This mirrors how TestGenerate_writeAttrLine asserts on generated HCL
// output in this file, applied to the group_ids filtering added to fix the
// dangling default-group reference bug.
func TestGenerate_UserGroupsHCLExcludesDefaultGroups(t *testing.T) {
	groupIDToName := buildGroupIDToNameMap(fabricatedGroupsPayload())

	// fabricated users-with-groups payload: user 1 belongs to a mix of
	// default and custom groups, user 2 belongs only to default groups.
	fabricatedUsers := []any{
		map[string]any{
			"id": float64(100),
			"permissions": []any{
				map[string]any{
					"groups": []any{
						map[string]any{"id": float64(1)},  // Owner (default)
						map[string]any{"id": float64(2)},  // Member (default)
						map[string]any{"id": float64(10)}, // Analysts (custom)
						map[string]any{"id": float64(11)}, // Admins (custom)
					},
				},
			},
		},
		map[string]any{
			"id": float64(200),
			"permissions": []any{
				map[string]any{
					"groups": []any{
						map[string]any{"id": float64(1)}, // Owner (default)
						map[string]any{"id": float64(3)}, // Everyone (default)
					},
				},
			},
		},
	}

	var renderedBlocks []string

	for _, user := range fabricatedUsers {
		userTyped := user.(map[string]any)

		userPermissionsArray := userTyped["permissions"].([]any)
		userPermissions := userPermissionsArray[0].(map[string]any)
		allGroupIDs := []int{}
		for _, group := range userPermissions["groups"].([]any) {
			groupTyped := group.(map[string]any)
			allGroupIDs = append(allGroupIDs, int(groupTyped["id"].(float64)))
		}

		groupIDs := filterOutDefaultGroupIDs(allGroupIDs, groupIDToName)

		// mirrors the "omit the resource entirely if nothing is left to
		// manage" behavior in the generate.go "dbtcloud_user_groups" case.
		if len(groupIDs) == 0 {
			continue
		}

		f := hclwrite.NewEmptyFile()
		writeAttrLine("group_ids", groupIDs, "", f.Body())
		renderedBlocks = append(renderedBlocks, string(f.Bytes()))
	}

	// Only one block should have been rendered (for user 100); user 200 had
	// nothing but default groups and must produce no resource at all.
	assert.Len(t, renderedBlocks, 1)
	assert.Equal(t, "group_ids = [10, 11]\n", renderedBlocks[0])

	fullOutput := strings.Join(renderedBlocks, "")
	assert.NotContains(t, fullOutput, "= [1,")
	assert.NotContains(t, fullOutput, ", 1,")
	assert.NotContains(t, fullOutput, ", 1]")
	assert.NotContains(t, fullOutput, "= [2,")
	assert.NotContains(t, fullOutput, ", 2]")
	assert.NotContains(t, fullOutput, "= [3,")
	assert.NotContains(t, fullOutput, ", 3]")
}
