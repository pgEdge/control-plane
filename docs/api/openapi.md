# OpenAPI Specification

The Control Plane server serves a JSON OpenAPI v3 specification from the
`/v1/openapi.json` endpoint. It also implements [RFC
8631](https://datatracker.ietf.org/doc/html/rfc8631) for use with tools like
[Restish](https://rest.sh).

You can also access offline copies of the OpenAPI specification in the pgEdge Control Plane repository. We generate a few versions of the specification to accommodate different tools and use cases:

- [OpenAPI v3 YAML](https://github.com/pgEdge/control-plane/blob/main/api/apiv1/gen/http/openapi3.yaml)
- [OpenAPI v3 JSON](https://github.com/pgEdge/control-plane/blob/main/api/apiv1/gen/http//openapi3.json)
- [OpenAPI v2 YAML](https://github.com/pgEdge/control-plane/blob/main/api/apiv1/gen/http//openapi.yaml)
- [OpenAPI v2 JSON](https://github.com/pgEdge/control-plane/blob/main/api/apiv1/gen/http//openapi.json)