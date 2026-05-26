package output

import (
	"encoding/json"
	"fmt"
	"io"
)

type JSONFormatter struct {
	data any
}

func NewJSONFormatter(data any) *JSONFormatter {
	return &JSONFormatter{
		data: data,
	}
}

func (j *JSONFormatter) Write(out io.Writer) error {
	raw, err := json.MarshalIndent(j.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := out.Write(raw); err != nil {
		return fmt.Errorf("failed to write data to output: %w", err)
	}
	return nil
}

// func writeWithJqQuery(
// 	data []byte,
// 	query string,
// 	marshal func(v any) ([]byte, error),
// 	unmarshal func(data []byte, v any) error,
// 	out io.Writer,
// ) error {
// 	parsed, err := gojq.Parse(query)
// 	if err != nil {
// 		return fmt.Errorf("failed to parse query %q: %w", query, err)
// 	}
// 	var in any
// 	if err := unmarshal(data, in); err != nil {
// 		return fmt.Errorf("failed to unmarshal data: %w", err)
// 	}
// 	iter := parsed.Run(data)
// 	for {
// 		res, ok := iter.Next()
// 		if !ok {
// 			break
// 		}
// 		if err, ok := res.(error); ok {
// 			if err, ok := err.(*gojq.HaltError); ok && err.Value() == nil {
// 				break
// 			}
// 			return fmt.Errorf("encountered an error applying query: %w", err)
// 		}
// 		m, err := marshal(res)
// 		if err != nil {
// 			return fmt.Errorf("encountered an error marshalling query result: %w", err)
// 		}
// 		if _, err := out.Write(m); err != nil {
// 			return fmt.Errorf("encountered an error writing query result to output: %w", err)
// 		}
// 	}
// 	return nil
// }
