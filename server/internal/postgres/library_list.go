package postgres

import (
	"errors"
	"fmt"
	"strings"
)

// listWhitespace are the characters that Postgres's scanner_isspace treats as
// whitespace.
const listWhitespace = " \t\n\r\f\v"

// SplitLibraryList splits a list of library names or paths, such as the value
// of shared_preload_libraries, the same way that Postgres does. Names are
// separated by commas, and whitespace around each name is ignored. A name can
// be double-quoted to include commas or surrounding whitespace, and a doubled
// double quote within a quoted name represents a literal double quote. Unlike
// identifier lists, names are not lowercased. It returns an error if the list
// is malformed, since Postgres would fail to load it.
//
// This is ported from SplitDirectoriesString in Postgres's
// src/backend/utils/adt/varlena.c, except that paths are not canonicalized.
func SplitLibraryList(list string) ([]string, error) {
	rest := strings.TrimLeft(list, listWhitespace)
	if rest == "" {
		return nil, nil
	}

	var names []string
	for {
		var name string
		if strings.HasPrefix(rest, `"`) {
			// Quoted name: doubled quotes collapse into a single quote.
			var b strings.Builder
			rest = rest[1:]
			for {
				i := strings.IndexByte(rest, '"')
				if i < 0 {
					return nil, errors.New("unterminated quoted name")
				}
				b.WriteString(rest[:i])
				rest = rest[i+1:]
				if !strings.HasPrefix(rest, `"`) {
					break
				}
				b.WriteByte('"')
				rest = rest[1:]
			}
			name = b.String()
		} else {
			// Unquoted name: extends to the next comma or the end of the list,
			// excluding trailing whitespace.
			i := strings.IndexByte(rest, ',')
			if i < 0 {
				i = len(rest)
			}
			name = strings.TrimRight(rest[:i], listWhitespace)
			rest = rest[i:]
			if name == "" {
				return nil, errors.New("empty name")
			}
		}

		rest = strings.TrimLeft(rest, listWhitespace)
		switch {
		case rest == "":
			return append(names, name), nil
		case rest[0] == ',':
			names = append(names, name)
			rest = strings.TrimLeft(rest[1:], listWhitespace)
		default:
			return nil, fmt.Errorf("unexpected text after name %q", name)
		}
	}
}
