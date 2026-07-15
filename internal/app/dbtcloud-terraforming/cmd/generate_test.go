package cmd

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
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
		// NOTE: the "dbtcloud_account_features" cassette (testdata/dbtcloud/dbtcloud_account_features.yaml)
		// has not been recorded yet - recording it requires live account credentials via:
		//   OVERWRITE_VCR_CASSETTES=true go test ./internal/app/dbtcloud-terraforming/cmd/... \
		//     -run TestResourceGeneration/dbt_Cloud_account_features -v
		// Until that cassette exists, this row is scaffolded but will fail if run; see
		// TestGenerate_ComputeResourceLabel and TestGenerate_AccountFeaturesHCLEmission for
		// standalone unit coverage of the singleton ID handling and attribute emission in
		// the meantime.
		"dbt Cloud account features": {identifierType: "account", resourceType: "dbtcloud_account_features", testdataFilename: "dbtcloud_account_features"},
		// NOTE: the "dbtcloud_profile" cassette (testdata/dbtcloud/dbtcloud_profile.yaml)
		// has not been recorded yet - recording it requires live account credentials via:
		//   OVERWRITE_VCR_CASSETTES=true go test ./internal/app/dbtcloud-terraforming/cmd/... \
		//     -run TestResourceGeneration/dbt_Cloud_profiles -v
		// Until that cassette exists, this row is scaffolded but will fail if run; see
		// TestGenerate_ProfileHCLEmission and TestGenerate_TransformProfileForGenerate for
		// standalone unit coverage of the composite ID handling and attribute emission in
		// the meantime.
		"dbt Cloud profiles": {identifierType: "account", resourceType: "dbtcloud_profile", testdataFilename: "dbtcloud_profile"},
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

// TestGenerate_ComputeResourceLabel covers the generalized ID-derivation logic
// used to label generated `resource "..." "..."` blocks: the existing
// numeric/string id-based behavior for list-based resources must stay
// byte-identical, while a non-empty resourceIDOverride must let a resource
// (e.g. the dbtcloud_account_features singleton) opt out of id-based
// labelling entirely.
func TestGenerate_ComputeResourceLabel(t *testing.T) {
	tests := map[string]struct {
		resourceType       string
		structData         map[string]interface{}
		resourceIDOverride string
		want               string
	}{
		"numeric id, no override (existing behavior, e.g. dbtcloud_project)": {
			resourceType: "dbtcloud_project",
			structData:   map[string]interface{}{"id": float64(123)},
			want:         "terraform_managed_resource_123",
		},
		"string id, no override (existing behavior, e.g. dbtcloud_environment_variable)": {
			resourceType: "dbtcloud_environment_variable",
			structData:   map[string]interface{}{"id": "71_DBT_ENV"},
			want:         "terraform_managed_resource_71_DBT_ENV",
		},
		"singleton override takes precedence over structData id": {
			resourceType:       "dbtcloud_account_features",
			structData:         map[string]interface{}{"id": "1234"},
			resourceIDOverride: "account_features",
			want:               "terraform_managed_resource_account_features",
		},
		"singleton override with no id in structData at all": {
			resourceType:       "dbtcloud_account_features",
			structData:         map[string]interface{}{},
			resourceIDOverride: "account_features",
			want:               "terraform_managed_resource_account_features",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computeResourceLabel(tc.resourceType, tc.structData, tc.resourceIDOverride)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestGenerate_ComputeResourceLabelPanicsOnMissingID locks in the existing
// panic behavior for resources with no id and no override - this is the
// pre-existing guard against silently generating an unlabelled resource
// block, and must keep working for every currently-supported id-keyed
// resource type.
func TestGenerate_ComputeResourceLabelPanicsOnMissingID(t *testing.T) {
	assert.Panics(t, func() {
		computeResourceLabel("dbtcloud_project", map[string]interface{}{}, "")
	})
}

// fabricatedAccountFeaturesPayload mimics the single-element shape returned
// by dbtCloudClient.GetAccountFeatures(): a singleton object keyed by the
// account id rather than a per-item numeric id, with boolean feature flags
// matching the dbtcloud_account_features Terraform resource's attribute
// names.
func fabricatedAccountFeaturesPayload() map[string]interface{} {
	return map[string]interface{}{
		"id":                           "1234",
		"advanced_ci":                  true,
		"partial_parsing":              false,
		"repo_caching":                 true,
		"ai_features":                  true,
		"catalog_ingestion":            false,
		"explorer_account_ui":          true,
		"fusion_migration_permissions": false,
	}
}

// TestGenerate_AccountFeaturesHCLEmission feeds a fabricated account features
// payload through the same label-derivation and attribute-emission
// primitives (computeResourceLabel, writeAttrLine) that generate.go's
// schema-driven loop uses for every resource, and asserts that the resulting
// HCL carries the singleton resource label and the expected boolean
// attributes. This does not require a live account or the Terraform provider
// schema (see the scaffolded, not-yet-recorded "dbt Cloud account features"
// row in TestResourceGeneration for full end-to-end coverage once a cassette
// exists).
func TestGenerate_AccountFeaturesHCLEmission(t *testing.T) {
	features := fabricatedAccountFeaturesPayload()

	resourceLabel := computeResourceLabel("dbtcloud_account_features", features, "account_features")
	assert.Equal(t, "terraform_managed_resource_account_features", resourceLabel)

	f := hclwrite.NewEmptyFile()
	resource := f.Body().AppendNewBlock("resource", []string{"dbtcloud_account_features", resourceLabel}).Body()

	attrNames := make([]string, 0, len(features))
	for k := range features {
		if k == "id" {
			// id is never emitted as an attribute - it's only used for
			// import/state purposes, matching every other resource type.
			continue
		}
		attrNames = append(attrNames, k)
	}
	sort.Strings(attrNames)

	for _, attrName := range attrNames {
		writeAttrLine(attrName, features[attrName], "", resource)
	}

	output := string(f.Bytes())

	assert.Contains(t, output, `resource "dbtcloud_account_features" "terraform_managed_resource_account_features"`)
	// hclwrite aligns "=" signs to the longest attribute name in the block, so
	// match on the key/value pair rather than an exact single-space rendering.
	for _, want := range []string{
		"advanced_ci",
		"partial_parsing",
		"repo_caching",
		"ai_features",
		"catalog_ingestion",
		"explorer_account_ui",
		"fusion_migration_permissions",
	} {
		re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*%s\s*= (true|false)$`, regexp.QuoteMeta(want)))
		assert.Regexp(t, re, output, "expected attribute %q to be emitted", want)
	}
	assert.True(t, strings.Contains(output, "true") && strings.Contains(output, "false"), "expected both true and false flag values to be emitted")
	assert.NotRegexp(t, regexp.MustCompile(`(?m)^\s*id\s*=`), output, "the id field must not be emitted as an attribute")
}

// withLinkedResources temporarily sets the package-level listLinkedResources
// used by linkResource() for the duration of a test, restoring the previous
// value afterwards. Tests that don't call this leave linking off, matching
// linkResource's own "no resources configured -> false" default.
func withLinkedResources(t *testing.T, linked []string) {
	t.Helper()
	orig := listLinkedResources
	listLinkedResources = linked
	t.Cleanup(func() { listLinkedResources = orig })
}

// fabricatedProfilePayload mimics a single element of the []any returned by
// dbtCloudClient.GetProfiles(): a project-scoped profile with the attribute
// names confirmed against the terraform-provider-dbtcloud schema (key,
// connection_id, credentials_id - plural, extended_attributes_id optional).
func fabricatedProfilePayload() map[string]any {
	return map[string]any{
		"id":             float64(10),
		"project_id":     float64(5),
		"key":            "my-profile",
		"connection_id":  float64(20),
		"credentials_id": float64(30),
	}
}

// TestGenerate_TransformProfileForGenerate covers the dbtcloud_profile
// generate-time transform: the composite id folding (project_id into "id",
// plain id preserved as "profile_id") must happen regardless of linking, and
// linked references must only replace connection_id/credentials_id/
// project_id when the corresponding resource type is in
// --linked-resource-types.
func TestGenerate_TransformProfileForGenerate(t *testing.T) {
	t.Run("no linking: ids stay numeric except the folded composite id", func(t *testing.T) {
		profile := fabricatedProfilePayload()
		got := transformProfileForGenerate(profile)

		assert.Equal(t, "5_10", got["id"])
		assert.Equal(t, float64(10), got["profile_id"])
		assert.Equal(t, float64(5), got["project_id"])
		assert.Equal(t, float64(20), got["connection_id"])
		assert.Equal(t, float64(30), got["credentials_id"])
	})

	t.Run("linking dbtcloud_project and dbtcloud_global_connection rewrites references", func(t *testing.T) {
		withLinkedResources(t, []string{"dbtcloud_project", "dbtcloud_global_connection"})

		profile := fabricatedProfilePayload()
		got := transformProfileForGenerate(profile)

		assert.Equal(t, "5_10", got["id"], "the composite id must still be derived from the original numeric project_id")
		assert.Contains(t, got["project_id"], "dbtcloud_project.terraform_managed_resource_5.id")
		assert.Contains(t, got["connection_id"], "dbtcloud_global_connection.terraform_managed_resource_20.id")
	})

	t.Run("linking dbtcloud_snowflake_credential without embedded credentials type falls back to TBD", func(t *testing.T) {
		withLinkedResources(t, []string{"dbtcloud_snowflake_credential"})

		profile := fabricatedProfilePayload()
		got := transformProfileForGenerate(profile)

		assert.Equal(t, "---TBD---", got["credentials_id"])
	})

	t.Run("linking dbtcloud_snowflake_credential with embedded credentials type resolves the reference", func(t *testing.T) {
		withLinkedResources(t, []string{"dbtcloud_snowflake_credential"})

		profile := fabricatedProfilePayload()
		profile["credentials"] = map[string]any{"type": "snowflake", "adapter_version": ""}
		got := transformProfileForGenerate(profile)

		assert.Contains(t, got["credentials_id"], "dbtcloud_snowflake_credential.terraform_managed_resource_30.credential_id")
	})
}

// TestGenerate_ProfileHCLEmission feeds a fabricated profile payload through
// the same label-derivation and attribute-emission primitives
// (computeResourceLabel, writeAttrLine) that generate.go's schema-driven
// loop uses for every resource, asserting the resulting HCL carries the
// project-scoped composite label and the confirmed attribute names.
func TestGenerate_ProfileHCLEmission(t *testing.T) {
	profile := transformProfileForGenerate(fabricatedProfilePayload())

	resourceLabel := computeResourceLabel("dbtcloud_profile", profile, "")
	assert.Equal(t, "terraform_managed_resource_5_10", resourceLabel)

	f := hclwrite.NewEmptyFile()
	resource := f.Body().AppendNewBlock("resource", []string{"dbtcloud_profile", resourceLabel}).Body()

	// profile_id is a computed-only attribute in the real provider schema
	// (like credential_id on the credential resources), so it - like id -
	// would never reach writeAttrLine via generate.go's schema-driven loop.
	for _, attrName := range []string{"project_id", "key", "connection_id", "credentials_id"} {
		writeAttrLine(attrName, profile[attrName], "", resource)
	}

	output := string(f.Bytes())

	assert.Contains(t, output, `resource "dbtcloud_profile" "terraform_managed_resource_5_10"`)
	assert.Contains(t, output, `key`)
	assert.Contains(t, output, "connection_id")
	assert.Contains(t, output, "credentials_id")
	assert.NotRegexp(t, regexp.MustCompile(`(?m)^\s*id\s*=`), output, "the id field must not be emitted as an attribute")
	assert.NotRegexp(t, regexp.MustCompile(`(?m)^\s*profile_id\s*=`), output, "profile_id is computed-only and must not be emitted as an attribute")
}

// fabricatedEnvironmentPayload mimics a single element of the []any returned
// by dbtCloudClient.GetEnvironments(): withProfile controls whether the
// environment carries a primary_profile_id (profile-based account) or the
// legacy connection_id/credentials_id/extended_attributes_id trio
// (non-profile account) - never both, matching what the real API returns.
func fabricatedEnvironmentPayload(withProfile bool) map[string]any {
	env := map[string]any{
		"id":         float64(456),
		"project_id": float64(71),
		"name":       "prod",
	}
	if withProfile {
		env["primary_profile_id"] = float64(10)
	} else {
		env["connection_id"] = float64(20)
		env["credentials_id"] = float64(30)
		env["extended_attributes_id"] = float64(40)
	}
	return env
}

// TestGenerate_TransformEnvironmentForGenerate_EitherOr is the core
// regression test for the never-both invariant: a profile-bound environment
// must emit primary_profile_id and none of the legacy trio, a non-profile
// environment must be completely unchanged from the pre-existing behavior,
// and neither branch may ever leave both primary_profile_id and any of the
// legacy trio present at once.
func TestGenerate_TransformEnvironmentForGenerate_EitherOr(t *testing.T) {
	t.Run("profile-bound environment: primary_profile_id present, legacy trio absent", func(t *testing.T) {
		env := fabricatedEnvironmentPayload(true)
		got := transformEnvironmentForGenerate(env)

		assert.Contains(t, got, "primary_profile_id")
		assert.Equal(t, float64(10), got["primary_profile_id"], "without linking, primary_profile_id stays the plain numeric profile id")

		for _, legacyField := range []string{"connection_id", "credential_id", "credentials_id", "extended_attributes_id"} {
			assert.NotContains(t, got, legacyField, "legacy field %q must be absent whenever primary_profile_id is present", legacyField)
		}
	})

	t.Run("profile-bound environment with linking: primary_profile_id resolves to the profile resource, legacy trio still absent", func(t *testing.T) {
		withLinkedResources(t, []string{"dbtcloud_profile"})

		env := fabricatedEnvironmentPayload(true)
		got := transformEnvironmentForGenerate(env)

		assert.Contains(t, got["primary_profile_id"], "dbtcloud_profile.terraform_managed_resource_71_10.profile_id")
		for _, legacyField := range []string{"connection_id", "credential_id", "credentials_id", "extended_attributes_id"} {
			assert.NotContains(t, got, legacyField)
		}
	})

	t.Run("non-profile environment: legacy trio unchanged, primary_profile_id absent", func(t *testing.T) {
		env := fabricatedEnvironmentPayload(false)
		got := transformEnvironmentForGenerate(env)

		assert.NotContains(t, got, "primary_profile_id")
		assert.Equal(t, float64(20), got["connection_id"])
		assert.Equal(t, float64(30), got["credential_id"], "credentials_id is renamed to credential_id, matching pre-existing behavior")
		assert.Equal(t, float64(40), got["extended_attributes_id"])
	})
}
