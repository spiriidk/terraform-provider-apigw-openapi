package provider

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"apigw-openapi": providerserver.NewProtocol6WithError(New("test")()),
}

func run(t *testing.T, steps ...resource.TestStep) {
	t.Helper()
	resource.UnitTest(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_8_0)},
		ProtoV6ProviderFactories: factories,
		Steps:                    steps,
	})
}

// The golden case through Terraform: options given as an HCL object.
func TestRestAPIBody_golden(t *testing.T) {
	want, err := os.ReadFile("../oas/testdata/expected/basic/alb-authorizer.json")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := filepath.Abs("../oas/testdata/specs/basic.yaml")
	if err != nil {
		t.Fatal(err)
	}
	run(t, resource.TestStep{
		Config: `
output "body" {
  value = provider::apigw-openapi::rest_api_body(file("` + spec + `"), {
    integration = {
      base_uri                = "http://svc.example.com/api"
      connection_id           = "vpclink-a"
      integration_target      = "arn:aws:elasticloadbalancing:eu-west-1:123456789012:loadbalancer/app/internal/vpclink-a"
      preserve_forwarded_host = true
    }
    authorizer = {
      security_scheme                = "TokenAuthorizer"
      uri                            = "arn:aws:apigateway:eu-west-1:lambda:path/2015-03-31/functions/arn:aws:lambda:eu-west-1:123456789012:function:token-authorizer/invocations"
      credentials                    = "arn:aws:iam::123456789012:role/authorizer"
      result_ttl_in_seconds          = 0
      identity_validation_expression = "^Bearer [-0-9a-zA-Z\\._]*$"
      request_headers = [
        { name = "x-auth-operator-id", value = "context.authorizer.operatorId" },
        { name = "x-auth-company-id", value = "context.authorizer.companyId" },
        { name = "x-auth-whitelabel", value = "context.authorizer.whiteLabel" },
        { name = "Authorization", value = "context.authorizer.token" },
        { name = "x-auth-organisation_id", value = "context.authorizer.organisation_id" },
        { name = "x-auth-workspace_id", value = "context.authorizer.workspace_id" },
        { name = "x-auth-credential_id", value = "context.authorizer.credential_id" },
        { name = "x-auth-scope", value = "context.authorizer.scope" },
      ]
    }
    excluded_paths      = ["/v2/hidden"]
    catch_all_not_found = true
  })
}`,
		ConfigStateChecks: []statecheck.StateCheck{
			statecheck.ExpectKnownOutputValue("body", knownvalue.StringExact(string(want))),
		},
	})
}

func TestRestAPIBody_nullOptions(t *testing.T) {
	run(t, resource.TestStep{
		Config: `output "body" { value = provider::apigw-openapi::rest_api_body("paths: {}", null) }`,
		ConfigStateChecks: []statecheck.StateCheck{
			statecheck.ExpectKnownOutputValue("body", knownvalue.StringExact(`{"paths":{}}`)),
		},
	})
}

func TestRestAPIBody_errors(t *testing.T) {
	for name, tc := range map[string]struct{ config, err string }{
		"unknown attribute": {`{ integration = { base_url = "x" } }`, `integration: unsupported attribute\(s\) base_url`},
		"missing base_uri":  {`{ integration = {} }`, `integration.base_uri is required`},
		"wrong type":        {`{ excluded_paths = "x" }`, `excluded_paths must be a list of strings`},
		"render error":      {`{ excluded_paths = ["/nope"] }`, `excluded path /nope is not in the spec`},
	} {
		t.Run(name, func(t *testing.T) {
			run(t, resource.TestStep{
				Config:      `output "body" { value = provider::apigw-openapi::rest_api_body("paths: {}", ` + tc.config + `) }`,
				ExpectError: regexp.MustCompile(strings.ReplaceAll(tc.err, " ", `\s+`)),
			})
		})
	}
}
