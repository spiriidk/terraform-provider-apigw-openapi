resource "aws_api_gateway_rest_api" "this" {
  name              = "orders"
  put_rest_api_mode = "overwrite"

  body = provider::apigw-openapi::rest_api_body(file("${path.module}/openapi.yaml"), {
    integration = {
      base_uri                = "http://orders.internal.example.com"
      connection_id           = aws_apigatewayv2_vpc_link.this.id
      integration_target      = data.aws_lb.internal.arn
      preserve_forwarded_host = true
    }
    authorizer = {
      security_scheme                = "LambdaToken"
      uri                            = aws_lambda_function.authorizer.invoke_arn
      credentials                    = aws_iam_role.authorizer.arn
      result_ttl_in_seconds          = 300
      identity_validation_expression = "^Bearer [-0-9a-zA-Z\\._]*$"
      request_headers = [
        { name = "x-user-id", value = "context.authorizer.userId" },
      ]
    }
    excluded_paths      = ["/internal/health"]
    catch_all_not_found = true
  })

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}
