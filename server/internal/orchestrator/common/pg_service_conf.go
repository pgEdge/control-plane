package common

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*PgServiceConf)(nil)

const ResourceTypePgServiceConf resource.Type = "common.pg_service_conf"

func PgServiceConfResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePgServiceConf,
	}
}

type PgServiceConf struct {
	ParentID   string   `json:"parent_id"`
	HostID     string   `json:"host_id"`
	InstanceID string   `json:"instance_id"`
	NodeNames  []string `json:"node_names"`
	OwnerUID   int      `json:"owner_uid"`
	OwnerGID   int      `json:"owner_gid"`
}

func (p *PgServiceConf) ResourceVersion() string {
	return "1"
}

func (p *PgServiceConf) DiffIgnore() []string {
	return nil
}

func (p *PgServiceConf) Executor() resource.Executor {
	return resource.HostExecutor(p.HostID)
}

func (p *PgServiceConf) Identifier() resource.Identifier {
	return PgServiceConfResourceIdentifier(p.InstanceID)
}

func (p *PgServiceConf) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		filesystem.DirResourceIdentifier(p.ParentID),
		database.InstanceResourceIdentifier(p.InstanceID),
	}
	return deps
}

func (p *PgServiceConf) TypeDependencies() []resource.Type {
	return []resource.Type{database.ResourceTypeNode}
}

func (p *PgServiceConf) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, p.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	_, err = ReadResourceFile(fs, filepath.Join(parentFullPath, "pg_service.conf"))
	if err != nil {
		return fmt.Errorf("failed to read pg_service.conf: %w", err)
	}

	return nil
}

func (p *PgServiceConf) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	instance, err := resource.FromContext[*database.InstanceResource](rc, database.InstanceResourceIdentifier(p.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get instance %q: %w", p.InstanceID, err)
	}
	nodes, err := resource.AllFromContext[*database.NodeResource](rc, database.ResourceTypeNode)
	if err != nil {
		return fmt.Errorf("failed to get all nodes from state: %w", err)
	}

	conf := postgres.NewPgServiceConf()
	for _, node := range nodes {
		// We set an empty dbname here because service conf users will set the
		// database in their connection string, e.g. 'service=n1 dbname=my_app'.
		dsn, err := node.DSN(ctx, rc, instance, "")
		if err != nil {
			return fmt.Errorf("failed to get dsn for node %q: %w", node.Name, err)
		}
		conf.Services[node.Name] = dsn
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, p.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	path := filepath.Join(parentFullPath, "pg_service.conf")
	err = afero.WriteFile(fs, path, []byte(conf.String()), 0o600)
	if err != nil {
		return fmt.Errorf("failed to write pg_service.conf file '%s': %w", path, err)
	}
	if err := fs.Chown(path, p.OwnerUID, p.OwnerGID); err != nil {
		return fmt.Errorf("failed to change ownership for pg_service.conf file '%s': %w", path, err)
	}

	return nil
}

func (p *PgServiceConf) Update(ctx context.Context, rc *resource.Context) error {
	return p.Create(ctx, rc)
}

func (p *PgServiceConf) Delete(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	parentFullPath, err := filesystem.DirResourceFullPath(rc, p.ParentID)
	if err != nil {
		return fmt.Errorf("failed to get parent full path: %w", err)
	}

	err = fs.Remove(filepath.Join(parentFullPath, "pg_service.conf"))
	if errors.Is(err, afero.ErrFileNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove patroni.yaml: %w", err)
	}

	return nil
}
