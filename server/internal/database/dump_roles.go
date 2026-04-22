package database

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*DumpRolesResource)(nil)

const ResourceTypeDumpRoles resource.Type = "database.dump_roles"

func DumpRolesResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeDumpRoles,
		ID:   nodeName,
	}
}

type DumpRolesResource struct {
	NodeName   string   `json:"node_name"`
	Roles      []string `json:"roles"`
	Statements []string `json:"statements"`
}

func (r *DumpRolesResource) ResourceVersion() string {
	return "1"
}

func (r *DumpRolesResource) DiffIgnore() []string {
	return nil
}

func (r *DumpRolesResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.NodeName)
}

func (r *DumpRolesResource) Identifier() resource.Identifier {
	return DumpRolesResourceIdentifier(r.NodeName)
}

func (r *DumpRolesResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		NodeResourceIdentifier(r.NodeName),
	}
}

func (r *DumpRolesResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *DumpRolesResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *DumpRolesResource) Create(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}

	primary, err := GetPrimaryInstance(ctx, rc, r.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get primary instance: %w", err)
	}

	cmd := []string{
		"pg_dumpall",
		"--roles-only",
		"--username=pgedge",
		"--host=localhost",
		fmt.Sprintf("--port=%d", primary.ConnectionInfo.AdminPort),
	}
	var dump strings.Builder
	err = orch.ExecuteInstanceCommand(ctx, &dump, primary.Spec.DatabaseID, primary.Spec.InstanceID, cmd...)
	if err != nil {
		return fmt.Errorf("failed to execute pg_dumpall: %w", err)
	}

	r.Roles, r.Statements = SanitizeRolesDump(dump.String())

	return nil
}

func (r *DumpRolesResource) Update(ctx context.Context, rc *resource.Context) error {
	return r.Create(ctx, rc)
}

func (r *DumpRolesResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

var sqlStatementPattern = regexp.MustCompile(`(?i)^[a-z].+;$`)
var createRoleRegex = regexp.MustCompile(`(?i)^create role ([^;]+);`)

func SanitizeRolesDump(in string) ([]string, []string) {
	lines := strings.Split(in, "\n")

	var roles []string
	statements := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !sqlStatementPattern.MatchString(line) {
			continue
		}
		if f := createRoleRegex.FindStringSubmatch(line); len(f) > 1 {
			roles = append(roles, postgres.UnquoteIdentifier(f[1]))
		} else {
			statements = append(statements, line)
		}

	}

	return roles, statements
}
