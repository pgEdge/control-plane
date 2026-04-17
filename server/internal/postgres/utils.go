package postgres

import "strings"

// SetSafeIdentifiers ensures that the escaped identifiers produced by the
// QuoteIdentifier method are safe for the duration of the session.
func SetSafeIdentifiers() Statements {
	return Statements{
		Statement{
			SQL: `SET client_encoding = 'UTF8'`,
		},
		Statement{
			SQL: `SET standard_conforming_strings = on`,
		},
	}
}

// QuoteIdentifier quotes and sanitizes identifiers. Callers must execute the
// SetEncode statements to consider the output of this method safe.
func QuoteIdentifier(in string) string {
	out := strings.ReplaceAll(in, string([]byte{0}), "")
	return `"` + strings.ReplaceAll(out, `"`, `""`) + `"`
}

// UnquoteIdentifier removes quotes from a quoted identifier.
func UnquoteIdentifier(in string) string {
	out := strings.TrimPrefix(strings.TrimSuffix(in, `"`), `"`)
	return strings.ReplaceAll(out, `""`, `"`)
}
