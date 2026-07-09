package swarm

import (
	"regexp"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

// imageTagRegexp matches the pgEdge image tag format:
// {pgver}-spock{spockver}-{variant}-{build}
// e.g. 17.9-spock5.0.6-standard-2
var imageTagRegexp = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)-spock(\d+\.\d+(?:\.\d+)?)-`)

// parseImageTag extracts the Postgres and Spock versions from an image
// reference following the pgEdge tag format. Returns ok=false if the tag
// does not match the expected format (e.g. a dev build tag like "my-build").
func parseImageTag(image string) (pgVer, spockVer *ds.Version, ok bool) {
	tag := image
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		tag = image[idx+1:]
	}
	m := imageTagRegexp.FindStringSubmatch(tag)
	if m == nil {
		return nil, nil, false
	}
	pg, err := ds.ParseVersion(m[1])
	if err != nil {
		return nil, nil, false
	}
	spock, err := ds.ParseVersion(m[2])
	if err != nil {
		return nil, nil, false
	}
	return pg, spock, true
}

// versionHasPrefix reports whether tagVer starts with all components of specVer.
// Allows a spec declaring "5" to match a tag version of "5.0.6".
func versionHasPrefix(tagVer, specVer *ds.Version) bool {
	if len(tagVer.Components) < len(specVer.Components) {
		return false
	}
	for i, c := range specVer.Components {
		if tagVer.Components[i] != c {
			return false
		}
	}
	return true
}
