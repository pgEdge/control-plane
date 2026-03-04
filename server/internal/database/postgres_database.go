package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*PostgresDatabaseResource)(nil)

const ResourceTypePostgresDatabase resource.Type = "database.postgres_database"

func PostgresDatabaseResourceIdentifier(nodeName, dbName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName + "-" + dbName,
		Type: ResourceTypePostgresDatabase,
	}
}

type PostgresDatabaseResource struct {
	NodeName          string                `json:"node_name"`
	DatabaseName      string                `json:"database_name"`
	Owner             string                `json:"owner"`
	RenameFrom        string                `json:"rename_from"`
	HasRestoreConfig  bool                  `json:"hast_restore_config"`
	ExtraDependencies []resource.Identifier `json:"extra_dependencies"`
}

func (p *PostgresDatabaseResource) ResourceVersion() string {
	return "1"
}

func (p *PostgresDatabaseResource) DiffIgnore() []string {
	return nil
}

func (p *PostgresDatabaseResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(p.NodeName)
}

func (p *PostgresDatabaseResource) Identifier() resource.Identifier {
	return PostgresDatabaseResourceIdentifier(p.NodeName, p.DatabaseName)
}

func (p *PostgresDatabaseResource) Dependencies() []resource.Identifier {
	return slices.Concat(
		[]resource.Identifier{NodeResourceIdentifier(p.NodeName)},
		p.ExtraDependencies,
	)
}

func (p *PostgresDatabaseResource) TypeDependencies() []resource.Type {
	return nil
}

func (p *PostgresDatabaseResource) Refresh(ctx context.Context, rc *resource.Context) error {
	node, err := resource.FromContext[*NodeResource](rc, NodeResourceIdentifier(p.NodeName))
	if err != nil {
		return fmt.Errorf("failed to get node resource: %w", err)
	}
	primary, err := node.PrimaryInstance(ctx, rc)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}

	conn, err := primary.Connection(ctx, rc, p.DatabaseName)
	if postgres.IsDatabaseNotExists(err) {
		return fmt.Errorf("%w: database does not exist", resource.ErrNotFound)
	} else if err != nil {
		return fmt.Errorf("failed to connect to primary instance: %w", err)
	}
	defer conn.Close(ctx)

	// Check that Spock is enabled and that the spock node exists
	enabled, err := postgres.IsSpockEnabled().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if spock is enabled: %w", err)
	}
	if !enabled {
		return fmt.Errorf("%w: spock not enabled", resource.ErrNotFound)
	}

	needsCreate, err := postgres.NodeNeedsCreate(p.NodeName).Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if node needs to be created: %w", err)
	}
	if needsCreate {
		return resource.ErrNotFound
	}

	return nil
}

func (p *PostgresDatabaseResource) Create(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}

	node, err := resource.FromContext[*NodeResource](rc, NodeResourceIdentifier(p.NodeName))
	if err != nil {
		return fmt.Errorf("failed to get node resource: %w", err)
	}
	primary, err := node.PrimaryInstance(ctx, rc)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}
	dsn, err := orch.NodeDSN(ctx, rc, p.NodeName, node.PrimaryInstanceID, p.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to get node dsn: %w", err)
	}

	firstTimeSetup, err := p.isFirstTimeSetup(rc)
	if err != nil {
		return err
	}

	if firstTimeSetup && p.HasRestoreConfig {
		if err := p.postRestore(ctx, rc, primary, dsn); err != nil {
			return err
		}
	} else {
		if err := p.create(ctx, rc, primary, dsn); err != nil {
			return err
		}
	}

	if err := p.updateUserPrivileges(ctx, rc, primary); err != nil {
		return err
	}

	return nil
}

func (p *PostgresDatabaseResource) Update(ctx context.Context, rc *resource.Context) error {
	return p.Create(ctx, rc)
}

func (p *PostgresDatabaseResource) Delete(ctx context.Context, rc *resource.Context) error {
	// This is intentional. We don't want to delete any data or disrupt spock
	// operations outside of specific operations.
	return nil
}

func (p *PostgresDatabaseResource) postRestore(ctx context.Context, rc *resource.Context, primary *InstanceResource, dsn *postgres.DSN) error {
	if err := p.renameDB(ctx, rc, primary); err != nil {
		return err
	}

	conn, err := primary.Connection(ctx, rc, p.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer conn.Close(ctx)

	if err := p.validateIsPrimary(ctx, primary, conn); err != nil {
		return err
	}

	enabled, err := postgres.IsSpockEnabled().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if spock is enabled: %w", err)
	}

	if enabled {
		sets, err := postgres.GetReplicationSets().Structs(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to query replication sets: %w", err)
		}
		tabs, err := postgres.GetReplicationSetTables().Structs(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to query replication set tables: %w", err)
		}
		err = postgres.DropSpockAndCleanupSlots(p.DatabaseName).Exec(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to drop spock: %w", err)
		}
		err = postgres.InitializeSpockNode(p.NodeName, dsn).Exec(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to reinitialize spock: %w", err)
		}
		err = postgres.RestoreReplicationSets(sets, tabs).Exec(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to restore spock replication sets: %w", err)
		}
	} else {
		err = postgres.InitializeSpockNode(p.NodeName, dsn).Exec(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to initialize spock: %w", err)
		}
	}

	return nil
}

func (p *PostgresDatabaseResource) create(ctx context.Context, rc *resource.Context, primary *InstanceResource, dsn *postgres.DSN) error {
	if err := p.createDB(ctx, rc, primary); err != nil {
		return err
	}

	conn, err := primary.Connection(ctx, rc, p.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer conn.Close(ctx)

	if err := p.validateIsPrimary(ctx, primary, conn); err != nil {
		return err
	}

	err = postgres.InitializeSpockNode(p.NodeName, dsn).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to initialize spock: %w", err)
	}

	return nil
}

func (p *PostgresDatabaseResource) createDB(ctx context.Context, rc *resource.Context, primary *InstanceResource) error {
	conn, err := primary.Connection(ctx, rc, "postgres")
	if err != nil {
		return fmt.Errorf("failed to connect to 'postgres' database on primary instance: %w", err)
	}
	defer conn.Close(ctx)

	err = postgres.CreateDatabase(p.DatabaseName).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to create database '%s': %w", p.DatabaseName, err)
	}

	return nil
}

func (p *PostgresDatabaseResource) renameDB(ctx context.Context, rc *resource.Context, primary *InstanceResource) error {
	// Short circuit if we don't have a previous name or if the database name is
	// the same.
	if p.RenameFrom == "" || p.RenameFrom == p.DatabaseName {
		return nil
	}

	// This operation can be flaky because of other processes connected to the
	// database. We retry it a few times to avoid failing the entire create
	// operation.
	err := utils.Retry(3, 500*time.Millisecond, func() error {
		conn, err := primary.Connection(ctx, rc, "postgres")
		if err != nil {
			return fmt.Errorf("failed to connect to 'postgres' database on instance: %w", err)
		}
		defer conn.Close(ctx)

		return postgres.
			RenameDB(p.RenameFrom, p.DatabaseName).
			Exec(ctx, conn)
	})
	if err != nil {
		return fmt.Errorf("failed to rename database '%s' to '%s': %w", p.RenameFrom, p.DatabaseName, err)
	}

	return nil
}

func (p *PostgresDatabaseResource) updateUserPrivileges(ctx context.Context, rc *resource.Context, primary *InstanceResource) error {
	conn, err := primary.Connection(ctx, rc, p.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	err = postgres.EnableRepairMode().Exec(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to enable repair mode: %w", err)
	}

	if p.Owner != "" {
		err = postgres.AlterOwner(p.DatabaseName, p.Owner).Exec(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to set database owner to '%s': %w", p.Owner, err)
		}
	}

	err = postgres.GrantBuiltinRolePrivileges(postgres.BuiltinRolePrivilegeOptions{
		DBName: p.DatabaseName,
	}).Exec(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to grant privileges to built-in roles: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (p *PostgresDatabaseResource) isFirstTimeSetup(rc *resource.Context) (bool, error) {
	// This database will already exist in the state if it's been successfully
	// created before.
	_, err := resource.FromContext[*InstanceResource](rc, p.Identifier())
	if errors.Is(err, resource.ErrNotFound) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check state for previous version of this instance: %w", err)
	}

	return false, nil
}

func (p *PostgresDatabaseResource) validateIsPrimary(ctx context.Context, primary *InstanceResource, conn *pgx.Conn) error {
	isReplica, err := postgres.IsInRecovery().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check if instance is in recovery: %w", err)
	}
	if isReplica {
		return fmt.Errorf("unable to reconcile database: instance '%s' is replica", primary.Spec.InstanceID)
	}

	return nil
}
