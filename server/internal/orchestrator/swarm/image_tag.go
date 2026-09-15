package swarm

import (
	"github.com/pgEdge/control-plane/server/internal/ds"
)

// parseImageTag extracts the Postgres and Spock versions from an image
// reference following the pgEdge tag format. Returns ok=false if the tag
// does not match the expected format (e.g. a dev build tag like "my-build").
// See ds.ParseImageTag, which also backs version inference from a
// user-supplied image (database.PopulateSpecDefaults).
func parseImageTag(image string) (pgVer, spockVer *ds.Version, ok bool) {
	return ds.ParseImageTag(image)
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
