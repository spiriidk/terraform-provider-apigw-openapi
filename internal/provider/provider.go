// Package provider implements the apigw-openapi Terraform provider.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var (
	_ provider.Provider              = (*apigwOpenAPI)(nil)
	_ provider.ProviderWithFunctions = (*apigwOpenAPI)(nil)
)

type apigwOpenAPI struct {
	version string
}

// New returns a factory for the provider, reporting version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &apigwOpenAPI{version: version} }
}

func (p *apigwOpenAPI) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "apigw-openapi"
	resp.Version = p.version
}

func (p *apigwOpenAPI) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}

func (p *apigwOpenAPI) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}

func (p *apigwOpenAPI) Resources(context.Context) []func() resource.Resource { return nil }

func (p *apigwOpenAPI) DataSources(context.Context) []func() datasource.DataSource { return nil }

func (p *apigwOpenAPI) Functions(context.Context) []func() function.Function {
	return []func() function.Function{newRestAPIBody}
}
