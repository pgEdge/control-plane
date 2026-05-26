package configuration

import "github.com/goccy/go-yaml"

// YAMLParser implements a koanf YAML parser.
type YAMLParser struct{}

// NewYAMLParser returns a YAML Parser.
func NewYAMLParser() *YAMLParser {
	return &YAMLParser{}
}

// Unmarshal parses the given YAML bytes.
func (p *YAMLParser) Unmarshal(b []byte) (map[string]interface{}, error) {
	var out map[string]interface{}
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// Marshal marshals the given config map to YAML bytes.
func (p *YAMLParser) Marshal(o map[string]interface{}) ([]byte, error) {
	return yaml.Marshal(o)
}
