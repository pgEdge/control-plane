package apiv1

import "embed"

//go:embed gen/http/openapi3.json
var OpenAPISpecFS embed.FS
