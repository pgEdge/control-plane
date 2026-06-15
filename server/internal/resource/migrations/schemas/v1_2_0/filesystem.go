// produced by schematool 20b82249f8734cd7aa4ed88b2a0e60a68c7bf058 server/internal/filesystem DirResource
package v1_2_0

import (
	"github.com/pgEdge/control-plane/server/internal/resource"
	"os"
)

const ResourceTypeDir resource.Type = "filesystem.dir"

func DirResourceIdentifier(id string) resource.Identifier {
	return resource.Identifier{
		ID:   id,
		Type: ResourceTypeDir,
	}
}

type DirResource struct {
	ID       string      `json:"id"`
	ParentID string      `json:"parent_id"`
	HostID   string      `json:"host_id"`
	Path     string      `json:"path"`
	OwnerUID int         `json:"owner_uid"`
	OwnerGID int         `json:"owner_gid"`
	Perm     os.FileMode `json:"perm"`
	FullPath string      `json:"full_path"`
}
