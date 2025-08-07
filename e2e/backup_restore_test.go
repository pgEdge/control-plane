//go:build e2e_test

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

func TestPosixBackupRestore(t *testing.T) {
	t.Parallel()

	host1 := fixture.HostIDs()[0]
	tmpDir := fixture.TempDir(host1, t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Log("Creating database")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_backup_restore",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
					BackupConfig: &controlplane.BackupConfigSpec{
						Repositories: []*controlplane.BackupRepositorySpec{
							{
								Type:     client.RepositoryTypePosix,
								BasePath: pointerTo("/backups"),
							},
						},
					},
					OrchestratorOpts: &controlplane.OrchestratorOpts{
						Swarm: &controlplane.SwarmOpts{
							ExtraVolumes: []*controlplane.ExtraVolumesSpec{
								{
									HostPath:        tmpDir,
									DestinationPath: "/backups",
								},
							},
						},
					},
				},
			},
		},
	})

	opts := ConnectionOptions{
		Matcher:  WithNode("n1"),
		Username: "admin",
		Password: "password",
	}

	t.Log("Inserting test data")

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 1, "foo")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 2, "bar")
		require.NoError(t, err)
	})

	t.Log("Creating a full backup")

	db.BackupDatabaseNode(ctx, BackupDatabaseNodeOptions{
		Node: "n1",
		Options: &controlplane.BackupOptions{
			Type: "full",
		},
	})

	t.Log("Deleting all data")

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "DELETE FROM foo")
		require.NoError(t, err)
	})

	t.Log("Getting set name for latest full backup")

	setName := fixture.LatestPosixBackup(t, host1, tmpDir, string(db.ID))

	t.Log("Restoring to latest full backup")

	err := db.RestoreDatabase(ctx, RestoreDatabaseOptions{
		RestoreConfig: &controlplane.RestoreConfigSpec{
			SourceDatabaseID:   db.ID,
			SourceNodeName:     "n1",
			SourceDatabaseName: db.Spec.DatabaseName,
			Repository: &controlplane.RestoreRepositorySpec{
				Type:     client.RepositoryTypePosix,
				BasePath: pointerTo("/backups"),
			},
			RestoreOptions: map[string]string{
				"set": strings.TrimSpace(setName),
			},
		},
	})
	require.NoError(t, err)

	t.Log("Validating restored data")

	// Validate that our data is restored
	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		var count int

		row := conn.QueryRow(ctx, "SELECT COUNT(*) FROM foo")
		require.NoError(t, row.Scan(&count))

		assert.Equal(t, 2, count)
	})
}

func TestS3BackupRestore(t *testing.T) {
	t.Parallel()

	if !fixture.S3Enabled() {
		t.Skip("s3 not enabled for this fixture")
	}

	host1 := fixture.HostIDs()[0]
	dbID := uuid.NewString()

	t.Cleanup(func() {
		fixture.CleanupS3Backups(t, host1, dbID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Log("Creating database")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		ID: pointerTo(controlplane.Identifier(dbID)),
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_backup_restore",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:  pointerTo(0),
			Nodes: fixture.OneNodePerHost(),
			BackupConfig: &controlplane.BackupConfigSpec{
				Repositories: []*controlplane.BackupRepositorySpec{
					fixture.S3BackupRepository(),
				},
			},
		},
	})

	opts := ConnectionOptions{
		Matcher:  WithNode("n1"),
		Username: "admin",
		Password: "password",
	}

	t.Log("Inserting test data")

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 1, "foo")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 2, "bar")
		require.NoError(t, err)
	})

	t.Log("Creating a full backup")

	db.BackupDatabaseNode(ctx, BackupDatabaseNodeOptions{
		Node: "n1",
		Options: &controlplane.BackupOptions{
			Type: "full",
		},
	})

	t.Log("Deleting all data")

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "DELETE FROM foo")
		require.NoError(t, err)
	})

	t.Log("Getting set name for latest full backup")

	setName := fixture.LatestS3Backup(t, host1, string(db.ID))

	t.Log("Restoring to latest full backup")

	err := db.RestoreDatabase(ctx, RestoreDatabaseOptions{
		RestoreConfig: &controlplane.RestoreConfigSpec{
			SourceDatabaseID:   db.ID,
			SourceNodeName:     "n1",
			SourceDatabaseName: db.Spec.DatabaseName,
			Repository:         fixture.S3RestoreRepository(),
			RestoreOptions: map[string]string{
				"set": strings.TrimSpace(setName),
			},
		},
	})
	require.NoError(t, err)

	t.Log("Validating restored data")

	// Validate that our data is restored on a different node
	n2Opts := ConnectionOptions{
		Matcher:  WithNode("n2"),
		Username: "admin",
		Password: "password",
	}
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		var count int

		row := conn.QueryRow(ctx, "SELECT COUNT(*) FROM foo")
		require.NoError(t, row.Scan(&count))

		assert.Equal(t, 2, count)
	})
}

func TestS3AddNodeFromBackup(t *testing.T) {
	t.Parallel()

	if !fixture.S3Enabled() {
		t.Skip("s3 not enabled for this fixture")
	}

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	dbID := uuid.NewString()

	t.Cleanup(func() {
		fixture.CleanupS3Backups(t, host1, dbID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Log("Creating database")

	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		ID: pointerTo(controlplane.Identifier(dbID)),
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_backup_restore",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			BackupConfig: &controlplane.BackupConfigSpec{
				Repositories: []*controlplane.BackupRepositorySpec{
					fixture.S3BackupRepository(),
				},
			},
		},
	})

	opts := ConnectionOptions{
		Matcher:  WithNode("n1"),
		Username: "admin",
		Password: "password",
	}

	t.Log("Inserting test data")

	db.WithConnection(ctx, opts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 1, "foo")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 2, "bar")
		require.NoError(t, err)
	})

	t.Log("Creating a full backup")

	db.BackupDatabaseNode(ctx, BackupDatabaseNodeOptions{
		Node: "n1",
		Options: &controlplane.BackupOptions{
			Type: "full",
		},
	})

	t.Log("Adding a new node from backup")

	err := db.Update(ctx, UpdateOptions{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_backup_restore",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
				{
					Name:    "n2",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host2)},
					RestoreConfig: &controlplane.RestoreConfigSpec{
						SourceDatabaseID:   db.ID,
						SourceNodeName:     "n1",
						SourceDatabaseName: db.Spec.DatabaseName,
						Repository:         fixture.S3RestoreRepository(),
					},
				},
			},
			BackupConfig: &controlplane.BackupConfigSpec{
				Repositories: []*controlplane.BackupRepositorySpec{
					fixture.S3BackupRepository(),
				},
			},
		},
	})
	require.NoError(t, err)

	t.Log("Validating restored data")

	// Validate that our data is restored on the new node
	n2Opts := ConnectionOptions{
		Matcher:  WithNode("n2"),
		Username: "admin",
		Password: "password",
	}
	db.WithConnection(ctx, n2Opts, t, func(conn *pgx.Conn) {
		var count int

		row := conn.QueryRow(ctx, "SELECT COUNT(*) FROM foo")
		require.NoError(t, row.Scan(&count))

		assert.Equal(t, 2, count)
	})
}

func TestS3CreateDBFromBackup(t *testing.T) {
	t.Parallel()

	if !fixture.S3Enabled() {
		t.Skip("s3 not enabled for this fixture")
	}

	host1 := fixture.HostIDs()[0]
	host2 := fixture.HostIDs()[1]
	sourceDbID := uuid.NewString()

	t.Cleanup(func() {
		fixture.CleanupS3Backups(t, host1, sourceDbID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Log("Creating database")

	sourceDB := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		ID: pointerTo(controlplane.Identifier(sourceDbID)),
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_backup_restore",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
			BackupConfig: &controlplane.BackupConfigSpec{
				Repositories: []*controlplane.BackupRepositorySpec{
					fixture.S3BackupRepository(),
				},
			},
		},
	})

	sourceOpts := ConnectionOptions{
		Matcher:  WithNode("n1"),
		Username: "admin",
		Password: "password",
	}

	t.Log("Inserting test data")

	sourceDB.WithConnection(ctx, sourceOpts, t, func(conn *pgx.Conn) {
		_, err := conn.Exec(ctx, "CREATE TABLE foo (id INT PRIMARY KEY, val TEXT)")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 1, "foo")
		require.NoError(t, err)

		_, err = conn.Exec(ctx, "INSERT INTO foo (id, val) VALUES ($1, $2)", 2, "bar")
		require.NoError(t, err)
	})

	t.Log("Creating a full backup")

	sourceDB.BackupDatabaseNode(ctx, BackupDatabaseNodeOptions{
		Node: "n1",
		Options: &controlplane.BackupOptions{
			Type: "full",
		},
	})

	t.Log("Creating a new DB from backup")

	restoredDB := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_create_from_backup",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host2)},
				},
			},
			RestoreConfig: &controlplane.RestoreConfigSpec{
				SourceDatabaseID:   sourceDB.ID,
				SourceNodeName:     "n1",
				SourceDatabaseName: sourceDB.Spec.DatabaseName,
				Repository:         fixture.S3RestoreRepository(),
			},
		},
	})

	t.Log("Validating restored data")

	// Validate that our data is restored in the new database
	restoredOpts := ConnectionOptions{
		Matcher:  WithNode("n1"),
		Username: "admin",
		Password: "password",
	}
	restoredDB.WithConnection(ctx, restoredOpts, t, func(conn *pgx.Conn) {
		var count int

		row := conn.QueryRow(ctx, "SELECT COUNT(*) FROM foo")
		require.NoError(t, row.Scan(&count))

		assert.Equal(t, 2, count)
	})
}
