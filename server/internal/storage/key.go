package storage

import (
	"path"
	"strings"
)

// Prefix returns a slash-separated key prefix with a trailing slash to ensure
// that its safe for use in all storage operations.
func Prefix(elem ...string) string {
	p := path.Join(elem...) + "/"
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// Key returns a slash-separated key.
func Key(elem ...string) string {
	k := path.Join(elem...)
	if !strings.HasPrefix(k, "/") {
		k = "/" + k
	}
	return k
}

// ensureTrailingSlash is a helper for prefix operations to ensure that the
// prefix ends with a trailing slash. This helps defend against improper uses of
// path.Join or fmt.Sprintf for producing prefixes.
func ensureTrailingSlash(prefix string) string {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}
