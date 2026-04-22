package database

import (
	"context"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*RolesSourceResource)(nil)

const ResourceTypeRolesSource resource.Type = "database.roles_source"

func RolesSourceResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeRolesSource,
		ID:   nodeName,
	}
}

type RolesSourceResource struct {
	NodeName       string `json:"node_name"`
	SourceNodeName string `json:"source_node_name"`
}

func (r *RolesSourceResource) ResourceVersion() string {
	return "1"
}

func (r *RolesSourceResource) DiffIgnore() []string {
	return nil
}

func (r *RolesSourceResource) Executor() resource.Executor {
	return resource.AnyExecutor()
}

func (r *RolesSourceResource) Identifier() resource.Identifier {
	return RolesSourceResourceIdentifier(r.NodeName)
}

func (r *RolesSourceResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		DumpRolesResourceIdentifier(r.SourceNodeName),
	}
}

func (r *RolesSourceResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *RolesSourceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *RolesSourceResource) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *RolesSourceResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *RolesSourceResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
