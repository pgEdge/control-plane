package swarm

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*PgBackRestStanza)(nil)

var ResourceTypePgBackRestStanza resource.Type = "swarm.pgbackrest_stanza"

func PgBackRestStanzaIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypePgBackRestStanza,
	}
}

type PgBackRestStanza struct {
	NodeName string `json:"node_name"`
}

func (p *PgBackRestStanza) ResourceVersion() string {
	return "1"
}

func (p *PgBackRestStanza) DiffIgnore() []string {
	return nil
}

func (p *PgBackRestStanza) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeNode,
		ID:   p.NodeName,
	}
}

func (p *PgBackRestStanza) Identifier() resource.Identifier {
	return PgBackRestStanzaIdentifier(p.NodeName)
}

func (p *PgBackRestStanza) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		database.NodeResourceIdentifier(p.NodeName),
	}
}

func (p *PgBackRestStanza) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	node, err := resource.FromContext[*database.NodeResource](rc, database.NodeResourceIdentifier(p.NodeName))
	if err != nil {
		return fmt.Errorf("failed to get node %q: %w", p.NodeName, err)
	}

	infoCmd := PgBackRestBackupCmd("info", "--output=json").StringSlice()
	var output bytes.Buffer
	err = PostgresContainerExec(ctx, &output, client, node.PrimaryInstanceID, infoCmd)
	if err != nil {
		// pgbackrest info returns a 0 exit code even if the stanza doesn't
		// exist, so an error here means something else went wrong.
		return fmt.Errorf("failed to exec pgbackrest info: %w, output: %s", err, output.String())
	}
	info, err := pgbackrest.ParseInfoOutput(output.Bytes())
	if err != nil {
		return fmt.Errorf("failed to parse pgbackrest info output: %w, output: %s", err, output.String())
	}
	stanza := info.Stanza("db")
	if stanza == nil {
		// the stanza will be in the output even if it doesn't exist.
		return fmt.Errorf("stanza %q not found in pgbackrest info output", "db")
	}
	// This status code will be non-zero if the repository is empty, even if
	// it's otherwise configured correctly.
	if stanza.Status.Code != 0 && stanza.Status.Message != "no valid backups" {
		return resource.ErrNotFound
	}

	return nil
}

func (p *PgBackRestStanza) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	node, err := resource.FromContext[*database.NodeResource](rc, database.NodeResourceIdentifier(p.NodeName))
	if err != nil {
		return fmt.Errorf("failed to get node %q: %w", p.NodeName, err)
	}

	var stanzaCreateOut bytes.Buffer
	stanzaCreateCmd := PgBackRestBackupCmd("stanza-create", "--io-timeout=10s").StringSlice()
	err = PostgresContainerExec(ctx, &stanzaCreateOut, client, node.PrimaryInstanceID, stanzaCreateCmd)
	if err != nil {
		return fmt.Errorf("failed to exec pgbackrest stanza-create: %w, output: %s", err, stanzaCreateOut.String())
	}
	var checkOut bytes.Buffer
	checkCmd := PgBackRestBackupCmd("check").StringSlice()
	err = PostgresContainerExec(ctx, &checkOut, client, node.PrimaryInstanceID, checkCmd)
	if err != nil {
		return fmt.Errorf("failed to exec pgbackrest check: %w, output: %s", err, checkOut.String())
	}

	return nil
}

func (p *PgBackRestStanza) Update(ctx context.Context, rc *resource.Context) error {
	return p.Create(ctx, rc)
}

func (p *PgBackRestStanza) Delete(ctx context.Context, rc *resource.Context) error {
	// Removing the stanza will delete all backups, so we don't do this
	// automatically. Users can delete the stanza manually once the database is
	// deleted.
	return nil
}
