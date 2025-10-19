The Control Plane server serves a JSON OpenAPI v3 specification from the
`/v1/openapi.json` endpoint. It also implements [RFC
8631](https://datatracker.ietf.org/doc/html/rfc8631) for use with tools like
[Restish](https://rest.sh).

You can also access offline copies of the OpenAPI specification in this
repository. We generate a few versions of the specification to accommodate
different tools and use cases:

- [OpenAPI v3 YAML](../api/apiv1/gen/http/openapi3.yaml)
- [OpenAPI v3 JSON](../api/apiv1/gen/http/openapi3.json)
- [OpenAPI v2 YAML](../api/apiv1/gen/http/openapi.yaml)
- [OpenAPI v2 JSON](../api/apiv1/gen/http/openapi.json)

If you've cloned this repository and have Docker installed, you can run this
command to start a local API documentation server on http://localhost:8999:

```sh
make api-docs
```
