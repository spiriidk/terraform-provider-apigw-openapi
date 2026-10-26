# terraform-provider-apigw-openapi

A Terraform provider with one function, `rest_api_body`. It renders an Amazon API Gateway
REST API body from an OpenAPI document: local `$ref`s inlined, `HTTP_PROXY` integrations
(VPC link v1 or v2), a Lambda token authorizer, path rewrites and a catch-all 404.

Plain HCL can't do this: it has no recursion to inline `$ref`s, and `jsonencode` sorts
keys, so the body would change, and the API redeploy, whenever key order differs.

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

Requires Terraform 1.8 or later. Reference: [docs/functions/rest_api_body.md](docs/functions/rest_api_body.md).

## Compatibility

Output matches, byte for byte, a JavaScript implementation that parses with the `yaml`
npm package and serialises with `JSON.stringify`: YAML 1.2 core schema, JavaScript
property order (integer-like keys first) and number formatting. The golden tests in
`internal/oas/testdata` pin this.

Rejected, though rare in OpenAPI documents: collection map keys (`? [a, b]: c`), the
`!!binary`, `!!timestamp`, `!!set`, `!!omap` and `!!pairs` tags, and bodies over 64 MiB.

## Development

```sh
go test ./...        # function tests need terraform >= 1.8 on PATH
go generate ./...    # regenerates docs/
go test -run '^$' -fuzz FuzzRender ./internal/oas/
```

To compare with bodies produced elsewhere, write a cases file in the format of
`internal/oas/testdata/cases.json` and run
`PARITY_CASES=<file> go test ./internal/oas/ -run TestParity`.

## Releasing

Push a `vX.Y.Z` tag. The release workflow builds with GoReleaser and signs the checksums
with the GPG key in the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets; the Terraform
Registry publishes the release from there.

## License

[MPL-2.0](LICENSE)
