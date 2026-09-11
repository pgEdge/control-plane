package ds

import (
	"regexp"
	"strings"
)

// imageTagRegexp matches the pgEdge image tag format:
// {pgver}-spock{spockver}-{variant}[-{build}]
// e.g. 17.9-spock5.0.6-standard-2, 17.10-spock5-standard
// The Postgres version requires at least major.minor; the Spock version may be
// major-only (e.g. spock5) to support mutable channel tags.
var imageTagRegexp = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)-spock(\d+(?:\.\d+){0,2})-`)

// ParseImageTag extracts the Postgres and Spock versions from an image
// reference following the pgEdge tag format. Returns ok=false if the tag
// does not match the expected format (e.g. a dev build tag like "my-build").
// Digest suffixes (e.g. @sha256:…) are stripped before parsing.
func ParseImageTag(image string) (pgVer, spockVer *Version, ok bool) {
	// Strip any digest suffix before extracting the tag.
	if idx := strings.Index(image, "@"); idx >= 0 {
		image = image[:idx]
	}
	tag := image
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		tag = image[idx+1:]
	}
	m := imageTagRegexp.FindStringSubmatch(tag)
	if m == nil {
		return nil, nil, false
	}
	pg, err := ParseVersion(m[1])
	if err != nil {
		return nil, nil, false
	}
	spock, err := ParseVersion(m[2])
	if err != nil {
		return nil, nil, false
	}
	return pg, spock, true
}
