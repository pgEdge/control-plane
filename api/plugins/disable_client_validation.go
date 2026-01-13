package plugins

import (
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/eval"
)

func init() {
	codegen.RegisterPlugin("disable-client-side-response-validation", "gen", nil, Generate)
}

func Generate(genpkg string, roots []eval.Root, files []*codegen.File) ([]*codegen.File, error) {
	for _, file := range files {
		if strings.HasSuffix(file.Path, "types.go") {
			for _, s := range file.Section("client-validate") {
				s.Source = noopValidatePrefix + s.Source + noopValidateSuffix
				if s.FuncMap == nil {
					s.FuncMap = map[string]any{}
				}
				s.FuncMap["hasSuffix"] = strings.HasSuffix
			}
		}
	}
	return files, nil
}

const noopValidatePrefix = `
{{ if hasSuffix .VarName "ResponseBody" }}
{{ printf "Validate%s runs a no-op validation on %s" .VarName .Name | comment }}
func Validate{{ .VarName }}(body {{ .Ref }}) (err error) {
	return 
}
{{ else }}
`

const noopValidateSuffix = `
{{ end }}
`
