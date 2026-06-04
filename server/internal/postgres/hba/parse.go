package hba

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode"
)

// This file provides lightweight parsers for pg_hba.conf and pg_ident.conf
// lines. They are used to validate user-supplied entries at spec-acceptance
// time.
//
// Validation is intentionally minimal. Per the design, the control plane does
// not second-guess the operator: there are NO enum-style restrictions on the
// auth method, no blocking of trust on broad CIDRs, no duplicate detection, and
// no map= cross-reference checks. We only reject lines that are not
// recognizable pg_hba/pg_ident entries at all (unknown connection type, too few
// fields, unterminated quotes). PostgreSQL surfaces deeper configuration errors
// (bad auth methods, addresses, options) at reload time via the task logs.

var entryTypes = map[EntryType]struct{}{
	EntryTypeLocal:           {},
	EntryTypeHost:            {},
	EntryTypeHostSSL:         {},
	EntryTypeHostNoSSL:       {},
	EntryTypeHostGSSEnc:      {},
	EntryTypeHostNoGSSEnc:    {},
	EntryTypeInclude:         {},
	EntryTypeIncludeIfExists: {},
	EntryTypeIncludeDir:      {},
}

func isIncludeType(t EntryType) bool {
	return t == EntryTypeInclude || t == EntryTypeIncludeIfExists || t == EntryTypeIncludeDir
}

// isBareIP reports whether s is a plain IP address with no CIDR suffix. It is
// used only to disambiguate the optional separate IP-mask form of a host entry
// ("ADDRESS MASK METHOD"); it is not a validity check on the address.
func isBareIP(s string) bool {
	return !strings.Contains(s, "/") && net.ParseIP(s) != nil
}

// IsComment reports whether a line is blank or a comment and therefore carries
// no rule to parse. Callers should skip such lines.
func IsComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "#")
}

// tokenize splits a configuration line into whitespace-separated fields,
// honoring double-quoted strings (which may contain spaces) and stopping at an
// unquoted '#' comment. It returns an error for an unterminated quote.
func tokenize(line string) ([]string, error) {
	var (
		tokens   []string
		buf      strings.Builder
		inQuote  bool
		hasToken bool
	)
	flush := func() {
		if hasToken {
			tokens = append(tokens, buf.String())
			buf.Reset()
			hasToken = false
		}
	}
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
			hasToken = true
		case r == '#' && !inQuote:
			flush()
			return tokens, nil
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			buf.WriteRune(r)
			hasToken = true
		}
	}
	if inQuote {
		return nil, errors.New("unterminated quoted string")
	}
	flush()
	return tokens, nil
}

// ParseEntry parses a single pg_hba.conf rule line into an Entry. The line must
// contain a rule; callers should skip blank and comment lines (see IsComment).
// The auth method is captured verbatim and NOT validated against a known set.
func ParseEntry(line string) (Entry, error) {
	tokens, err := tokenize(line)
	if err != nil {
		return Entry{}, err
	}
	if len(tokens) == 0 {
		return Entry{}, errors.New("empty entry")
	}

	typ := EntryType(tokens[0])
	if _, ok := entryTypes[typ]; !ok {
		return Entry{}, fmt.Errorf("unknown connection type %q", tokens[0])
	}

	switch {
	case isIncludeType(typ):
		if len(tokens) != 2 {
			return Entry{}, fmt.Errorf("%s requires exactly one path argument", typ)
		}
		return Entry{Type: typ, IncludePath: tokens[1]}, nil

	case typ == EntryTypeLocal:
		// local DATABASE USER METHOD [options]
		if len(tokens) < 4 {
			return Entry{}, errors.New("local entry requires database, user, and an auth method")
		}
		return Entry{
			Type:        typ,
			Database:    tokens[1],
			User:        tokens[2],
			AuthMethod:  AuthMethod(tokens[3]),
			AuthOptions: strings.Join(tokens[4:], " "),
		}, nil

	default:
		// host-based: TYPE DATABASE USER ADDRESS [MASK] METHOD [options]
		if len(tokens) < 5 {
			return Entry{}, fmt.Errorf("%s entry requires database, user, address, and an auth method", typ)
		}
		entry := Entry{
			Type:     typ,
			Database: tokens[1],
			User:     tokens[2],
			Address:  tokens[3],
		}
		rest := tokens[4:]
		if len(rest) >= 2 && isBareIP(rest[0]) {
			// Separate IP-mask form: ADDRESS MASK METHOD [options].
			entry.Mask = rest[0]
			entry.AuthMethod = AuthMethod(rest[1])
			entry.AuthOptions = strings.Join(rest[2:], " ")
		} else {
			entry.AuthMethod = AuthMethod(rest[0])
			entry.AuthOptions = strings.Join(rest[1:], " ")
		}
		return entry, nil
	}
}

// IdentEntry is a single pg_ident.conf user-name-map line: a mapping of
// map-name + system-username to a PostgreSQL username, or an include directive.
type IdentEntry struct {
	MapName          string
	SystemUsername   string
	PostgresUsername string

	// Include and IncludePath are set instead of the mapping fields when the
	// line is an include directive.
	Include     EntryType
	IncludePath string
}

func (e IdentEntry) String() string {
	if e.Include != "" {
		return fmt.Sprintf("%-17s %s", e.Include, e.IncludePath)
	}
	return fmt.Sprintf("%-15s %-23s %s", e.MapName, e.SystemUsername, e.PostgresUsername)
}

// ParseIdent parses a single pg_ident.conf line. The line must contain a
// mapping or an include directive; callers should skip blank and comment lines
// (see IsComment). A mapping is always exactly three fields.
func ParseIdent(line string) (IdentEntry, error) {
	tokens, err := tokenize(line)
	if err != nil {
		return IdentEntry{}, err
	}
	if len(tokens) == 0 {
		return IdentEntry{}, errors.New("empty entry")
	}

	if typ := EntryType(tokens[0]); isIncludeType(typ) {
		if len(tokens) != 2 {
			return IdentEntry{}, fmt.Errorf("%s requires exactly one path argument", typ)
		}
		return IdentEntry{Include: typ, IncludePath: tokens[1]}, nil
	}

	if len(tokens) != 3 {
		return IdentEntry{}, errors.New("pg_ident entry requires map-name, system-username, and postgres-username")
	}
	return IdentEntry{
		MapName:          tokens[0],
		SystemUsername:   tokens[1],
		PostgresUsername: tokens[2],
	}, nil
}
