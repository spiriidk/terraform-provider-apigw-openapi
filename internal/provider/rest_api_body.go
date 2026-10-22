package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/spiriidk/terraform-provider-apigw-openapi/internal/oas"
)

var _ function.Function = (*restAPIBody)(nil)

type restAPIBody struct{}

func newRestAPIBody() function.Function { return &restAPIBody{} }

func (f *restAPIBody) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "rest_api_body"
}

func (f *restAPIBody) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Render an API Gateway REST API body from an OpenAPI document",
		MarkdownDescription: "Inlines local `$ref`s and adds integrations, a Lambda token authorizer and a catch-all 404. " +
			"Returns JSON for `aws_api_gateway_rest_api.body`, keeping the document's key order.",
		Parameters: []function.Parameter{
			function.StringParameter{
				Name:                "spec",
				MarkdownDescription: "OpenAPI document, YAML or JSON, e.g. `file(\"openapi.yaml\")`.",
			},
			function.DynamicParameter{
				Name:                "options",
				AllowNullValue:      true,
				MarkdownDescription: "Object; see [`options`](#options).",
			},
		},
		Return: function.StringReturn{},
	}
}

func (f *restAPIBody) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var spec string
	var options types.Dynamic
	if resp.Error = req.Arguments.Get(ctx, &spec, &options); resp.Error != nil {
		return
	}
	raw, err := plain(options)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(1, "value"+err.Error())
		return
	}
	opts, err := oas.ParseOptions(raw)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(1, err.Error())
		return
	}
	body, err := oas.Render(spec, opts)
	if err != nil {
		resp.Error = function.NewFuncError(err.Error())
		return
	}
	resp.Error = resp.Result.Set(ctx, body)
}

// plain converts a Terraform value to the form oas.ParseOptions takes.
// Terraform core resolves unknown arguments before calling the function.
func plain(v attr.Value) (any, error) {
	if v == nil || v.IsNull() {
		return nil, nil
	}
	if v.IsUnknown() {
		return nil, fmt.Errorf(" is unknown")
	}
	switch x := v.(type) {
	case basetypes.DynamicValue:
		return plain(x.UnderlyingValue())
	case basetypes.StringValue:
		return x.ValueString(), nil
	case basetypes.BoolValue:
		return x.ValueBool(), nil
	case basetypes.NumberValue:
		f, _ := x.ValueBigFloat().Float64()
		return f, nil
	case basetypes.ObjectValue:
		return plainMap(x.Attributes())
	case basetypes.MapValue:
		return plainMap(x.Elements())
	case basetypes.ListValue:
		return plainList(x.Elements())
	case basetypes.TupleValue:
		return plainList(x.Elements())
	case basetypes.SetValue:
		return plainList(x.Elements())
	}
	return nil, fmt.Errorf(" has unsupported type %s", v.Type(context.Background()))
}

func plainMap(m map[string]attr.Value) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		p, err := plain(v)
		if err != nil {
			return nil, fmt.Errorf(".%s%w", k, err)
		}
		out[k] = p
	}
	return out, nil
}

func plainList(l []attr.Value) ([]any, error) {
	out := make([]any, len(l))
	for i, v := range l {
		p, err := plain(v)
		if err != nil {
			return nil, fmt.Errorf("[%d]%w", i, err)
		}
		out[i] = p
	}
	return out, nil
}
