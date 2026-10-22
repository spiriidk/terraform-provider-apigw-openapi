# terraform-provider-apigw-openapi

Terraform provider with one function, `rest_api_body`, that renders an Amazon API Gateway
REST API body from an OpenAPI document: local `$ref`s inlined, `HTTP_PROXY` integrations
(VPC link v1 or v2), a Lambda token authorizer, path rewrites and a catch-all 404.

Terraform alone can't do this: HCL has no recursion to inline `$ref`s, and `jsonencode`
sorts keys, so every reorder would redeploy the API.

```terraform
terraform {
  required_providers {
    apigw-openapi = {
      source = "spiriidk/apigw-openapi"
    }
  }
}

resource "aws_api_gateway_rest_api" "this" {
  name = "orders"
  body = provider::apigw-openapi::rest_api_body(file("openapi.yaml"), {
    integration = {
      base_uri      = "http://orders.internal.example.com"
      connection_id = "abc123"
    }
    catch_all_not_found = true
  })
}
```

Requires Terraform 1.8 or later. Full reference: [docs/functions/rest_api_body.md](docs/functions/rest_api_body.md).

## Compatibility

Output matches, byte for byte, a JavaScript implementation that parses with the `yaml`
npm package and serialises with `JSON.stringify`: YAML 1.2 core schema, JavaScript property
order (integer-like keys first, then insertion order) and JavaScript number formatting.
The golden tests in `internal/oas/testdata` hold it to that.

Known differences, both for input that is unusual in OpenAPI documents:

- Collection map keys (`? [a, b]: c`) are rejected.
- `!!binary`, `!!timestamp`, `!!set`, `!!omap` and `!!pairs` tags are rejected.

## Development

```sh
go test ./...                 # needs terraform >= 1.8 on PATH for the function tests
go generate ./...             # regenerates docs/
go test -run '^$' -fuzz FuzzRender ./internal/oas/
```

`PARITY_CASES=<cases.json> go test ./internal/oas/ -run TestParity` compares against
bodies produced elsewhere, in the format of `internal/oas/testdata/cases.json`.

## Releasing

Push a `vX.Y.Z` tag. The release workflow builds with GoReleaser and signs the checksums
with the GPG key in the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets; the Terraform Registry
picks the release up from there.

## License

[MPL-2.0](LICENSE)
