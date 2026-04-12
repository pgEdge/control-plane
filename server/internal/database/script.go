package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/rs/zerolog"
	"github.com/samber/do"
)

type ScriptName string

func (s ScriptName) String() string {
	return string(s)
}

const (
	ScriptNamePostInit           ScriptName = "post_init"
	ScriptNamePostDatabaseCreate ScriptName = "post_database_create"
)

type Script struct {
	DatabaseID string     `json:"database_id"`
	NodeName   string     `json:"node_name"`
	Name       ScriptName `json:"name"`
	Statements []string   `json:"statements"`
	Succeeded  bool       `json:"succeeded"`
	// NeedsToRun is used to trigger a resource diff. Resources should call
	// SetScriptNeedsToRun to set it.
	NeedsToRun bool `json:"needs_to_run"`
}

type Scripts map[ScriptName]*Script

type ScriptResult struct {
	DatabaseID  string     `json:"database_id"`
	ScriptName  ScriptName `json:"script_name"`
	NodeName    string     `json:"node_name"`
	Succeeded   bool       `json:"succeeded"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
	Error       string     `json:"error"`
}

func NewScriptResult(databaseID string, scriptName ScriptName, nodeName string) *ScriptResult {
	return &ScriptResult{
		DatabaseID: databaseID,
		ScriptName: scriptName,
		NodeName:   nodeName,
	}
}

func (s *ScriptResult) Validate() error {
	if s == nil {
		return errors.New("cannot be nil")
	}
	if s.DatabaseID == "" {
		return errors.New("database ID cannot be empty")
	}
	if s.ScriptName == "" {
		return errors.New("script name cannot be empty")
	}
	if s.NodeName == "" {
		return errors.New("node name cannot be empty")
	}

	return nil
}

func ExecuteScript(ctx context.Context, rc *resource.Context, conn *pgx.Conn, script *Script) error {
	ok, err := checkScriptPreconditions(rc, script)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	svc, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	logger = logger.With().
		Str("database_id", script.DatabaseID).
		Str("node_name", script.NodeName).
		Stringer("script_name", script.Name).
		Logger()

	result, err := svc.GetScriptResult(ctx, script.DatabaseID, script.Name, script.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get script result: %w", err)
	}
	if result.Succeeded {
		return nil
	}

	logger.Info().Msg("executing script")

	// We're intentionally not using repair mode here so that any tables created
	// by these scripts end up in the replication set. This does not cause
	// conflicts because we execute scripts only once, before any subscriptions
	// are created.
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	result.StartedAt = time.Now()

	var errs []error
	for i, statement := range script.Statements {
		_, err := tx.Exec(ctx, statement)
		if err != nil {
			err = fmt.Errorf("failed to execute %s[%d]: %w", script.Name, i, err)
			result.Error = err.Error()
			errs = append(errs, err)
			break
		}
	}

	if len(errs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to commit transaction: %w", err))
		}
	}

	result.CompletedAt = time.Now()
	result.Succeeded = len(errs) == 0
	if result.Succeeded {
		result.Error = ""
	}

	elapsed := result.CompletedAt.Sub(result.StartedAt)
	logger.Info().
		Float64("duration_seconds", elapsed.Seconds()).
		Bool("succeeded", result.Succeeded).
		Msg("script completed")

	if err := svc.UpdateScriptResult(ctx, result); err != nil {
		errs = append(errs, fmt.Errorf("failed to store script result: %w", err))
	}

	return errors.Join(errs...)
}

func IsDatabaseNotCreated(rc *resource.Context) (bool, error) {
	databaseNotCreated, err := resource.VariableFromContext[bool](rc, VariableNameDatabaseNotCreated)
	if errors.Is(err, resource.ErrVariableUndefined) {
		// Default to false when undefined
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check database not created: %w", err)
	}
	return databaseNotCreated, nil
}

func SetScriptNeedsToRun(ctx context.Context, rc *resource.Context, script *Script) error {
	ok, err := checkScriptPreconditions(rc, script)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	script.NeedsToRun = true
	return nil
}

func checkScriptPreconditions(rc *resource.Context, script *Script) (bool, error) {
	if script == nil {
		return false, nil
	}
	if script.Succeeded {
		return false, nil
	}
	databaseNotCreated, err := IsDatabaseNotCreated(rc)
	if err != nil {
		return false, err
	}
	return databaseNotCreated, nil
}
