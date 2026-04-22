package utils

import (
	"reflect"
	"strings"
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

func TestValidateID(t *testing.T) {
	valid := []string{
		"a",                                    // single char
		"ab",                                   // two chars
		"my-db",                                // typical short ID
		"production",                           // letters only
		"db-1",                                 // trailing digit
		"1-db",                                 // leading digit
		"abcdefghijklmnopqrstuvwxyz1234567890", // 36 chars (max)
		"76f9b8c0-4958-11f0-a489-3bb29577c696", // UUID (36 chars)
	}
	for _, id := range valid {
		t.Run("valid/"+id, func(t *testing.T) {
			if err := ValidateID(id); err != nil {
				t.Errorf("ValidateID(%q) = %v, want nil", id, err)
			}
		})
	}

	invalid := []string{
		"",                                      // empty
		strings.Repeat("a", 37),                 // 37 chars (one over max)
		"UPPERCASE",                             // uppercase letters
		"-leading-hyphen",                       // leading hyphen
		"trailing-hyphen-",                      // trailing hyphen
		"double--hyphen",                        // consecutive hyphens
		"has spaces",                            // spaces
		"has.dot",                               // dots
		"has_underscore",                        // underscores
	}
	for _, id := range invalid {
		t.Run("invalid/"+id, func(t *testing.T) {
			if err := ValidateID(id); err == nil {
				t.Errorf("ValidateID(%q) = nil, want error", id)
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
