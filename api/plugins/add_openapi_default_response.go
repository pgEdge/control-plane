package plugins

import (
	"path/filepath"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/eval"
	"goa.design/goa/v3/http/codegen/openapi"
	openapiv2 "goa.design/goa/v3/http/codegen/openapi/v2"
	openapiv3 "goa.design/goa/v3/http/codegen/openapi/v3"
)

func init() {
	codegen.RegisterPlugin("add-openapi-default-responses", "gen", nil, AddOpenAPIDefaultResponses)
}

func AddOpenAPIDefaultResponses(genpkg string, roots []eval.Root, files []*codegen.File) ([]*codegen.File, error) {
	for _, file := range files {
		switch filepath.Base(file.Path) {
		case "openapi.json", "openapi.yaml":
			processOpenAPIv2(file)
		case "openapi3.json", "openapi3.yaml":
			processOpenAPIv3(file)
		}
	}
	return files, nil
}

func processOpenAPIv2(file *codegen.File) {
	for _, s := range file.Section("openapi") {
		spec, ok := s.Data.(*openapiv2.V2)
		if !ok {
			continue
		}
		for _, p := range spec.Paths {
			path, ok := p.(*openapiv2.Path)
			if !ok {
				continue
			}
			addDefaultV2(path.Delete)
			addDefaultV2(path.Get)
			addDefaultV2(path.Head)
			addDefaultV2(path.Options)
			addDefaultV2(path.Patch)
			addDefaultV2(path.Post)
			addDefaultV2(path.Put)
		}
	}
}

func addDefaultV2(operation *openapiv2.Operation) {
	if operation == nil {
		return
	}
	if _, ok := operation.Responses["default"]; ok {
		return
	}
	if operation.Responses == nil {
		operation.Responses = make(map[string]*openapiv2.Response)
	}
	operation.Responses["default"] = &openapiv2.Response{
		Description: "Unexpected error response",
		Schema: &openapi.Schema{
			Ref: "#/definitions/APIError",
		},
	}
}

func processOpenAPIv3(file *codegen.File) {
	for _, s := range file.Section("openapi_v3") {
		spec, ok := s.Data.(*openapiv3.OpenAPI)
		if !ok {
			continue
		}
		for _, path := range spec.Paths {
			addDefaultV3(path.Connect)
			addDefaultV3(path.Delete)
			addDefaultV3(path.Get)
			addDefaultV3(path.Head)
			addDefaultV3(path.Options)
			addDefaultV3(path.Patch)
			addDefaultV3(path.Post)
			addDefaultV3(path.Put)
			addDefaultV3(path.Trace)
		}
	}
}

func addDefaultV3(operation *openapiv3.Operation) {
	if operation == nil {
		return
	}
	if _, ok := operation.Responses["default"]; ok {
		return
	}
	if operation.Responses == nil {
		operation.Responses = make(map[string]*openapiv3.ResponseRef)
	}
	desc := "Unexpected error response"
	operation.Responses["default"] = &openapiv3.ResponseRef{
		Value: &openapiv3.Response{
			Description: &desc,
			Content: map[string]*openapiv3.MediaType{
				"application/json": {
					Schema: &openapi.Schema{
						Ref: "#/components/schemas/APIError",
					},
					Example: map[string]string{
						"message": "A longer description of the error.",
						"name":    "error_name",
					},
				},
			},
		},
	}
}
