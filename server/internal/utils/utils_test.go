package utils

import (
	"reflect"
	"testing"
)

func TestBuildOptionArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected []string
	}{
		{
			name: "basic options",
			input: map[string]string{
				"type":   "full",
				"delta":  "",
				"verify": "yes",
			},
			expected: []string{
				"--type=full",
				"--delta",
				"--verify=yes",
			},
		},
		{
			name: "keys with -- already",
			input: map[string]string{
				"--type":  "diff",
				"--delta": "",
			},
			expected: []string{
				"--type=diff",
				"--delta",
			},
		},
		{
			name: "whitespace in values",
			input: map[string]string{
				"type":   "time",
				"target": "2025-06-11 09:00:00",
			},
			expected: []string{
				"--type=time",
				`--target=2025-06-11 09:00:00`,
			},
		},
		{
			name: "special characters and tabs/newlines",
			input: map[string]string{
				"note": "line1\nline2",
				"tab":  "one\t two",
			},
			expected: []string{
				"--note=line1\nline2",
				"--tab=one\t two",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := BuildOptionArgs(tt.input)
			if !reflect.DeepEqual(sortStrings(actual), sortStrings(tt.expected)) {
				t.Errorf("got %v, want %v", actual, tt.expected)
			}
		})
	}
}

func sortStrings(s []string) []string {
	sorted := append([]string(nil), s...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return sorted
}
