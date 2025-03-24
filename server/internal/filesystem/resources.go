package filesystem

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*DirResource](registry, ResourceTypeDir)
}
