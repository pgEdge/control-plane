package database

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*InstanceResource)(nil)

const ResourceTypeInstance resource.Type = "database.instance"

func InstanceResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeInstance,
	}
}

type InstanceResource struct {
	Spec                     *InstanceSpec              `json:"spec"`
	InstanceHostname         string                     `json:"instance_hostname"`
	PrimaryInstanceID        string                     `json:"primary_instance_id"`
	OrchestratorDependencies []resource.Identifier      `json:"dependencies"`
	ConnectionInfo           *ConnectionInfo            `json:"connection_info"`
	SpockRepsetBackup        *SpockRepsetBackupResource `json:"spock_repset_backup,omitempty"`
}

func (r *InstanceResource) ResourceVersion() string {
	return "1"
}

func (r *InstanceResource) DiffIgnore() []string {
	return []string{
		"/primary_instance_id",
		"/connection_info",
	}
}

func (r *InstanceResource) Executor() resource.Executor {
	return resource.HostExecutor(r.Spec.HostID)
}

func (r *InstanceResource) Identifier() resource.Identifier {
	return InstanceResourceIdentifier(r.Spec.InstanceID)
}

func (r *InstanceResource) Validate() error {
	var errs []error
	if r.Spec == nil {
		errs = append(errs, errors.New("spec: instance spec is required"))
	}
	return errors.Join(errs...)
}

func (r *InstanceResource) Dependencies() []resource.Identifier {
	dependencies := slices.Clone(r.OrchestratorDependencies)

	return dependencies
}

func (r *InstanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return resource.ErrNotFound
	}

	primaryInstanceID, err := GetPrimaryInstanceID(ctx, r.patroniClient(), 30*time.Second)
	if err != nil {
		return resource.ErrNotFound
	}
	r.PrimaryInstanceID = primaryInstanceID

	return nil
}

func (r *InstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	err := r.updateInstanceState(ctx, rc, &InstanceUpdateOptions{State: InstanceStateCreating})
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	err = r.initializeInstance(ctx, rc)
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Update(ctx context.Context, rc *resource.Context) error {
	err := r.updateInstanceState(ctx, rc, &InstanceUpdateOptions{State: InstanceStateModifying})
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}

	if err := r.patroniClient().Reload(ctx); err != nil {
		err = fmt.Errorf("failed to reload patroni conf: %w", err)
		return r.recordError(ctx, rc, err)
	}

	err = r.initializeInstance(ctx, rc)
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Delete(ctx context.Context, rc *resource.Context) error {
	svc, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	err = svc.DeleteInstance(ctx, r.Spec.DatabaseID, r.Spec.InstanceID)
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Connection(ctx context.Context, rc *resource.Context, dbName string) (*pgx.Conn, error) {
	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: r.ConnectionInfo.AdminDSN(dbName),
		TLS: tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %q: %w", r.Spec.DatabaseName, err)
	}
	return conn, nil
}

func (r *InstanceResource) initializeInstance(ctx context.Context, rc *resource.Context) error {
	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return err
	}

	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return err
	}

	patroniClient := r.patroniClient()
	err = WaitForPatroniRunning(ctx, patroniClient, 0)
	if err != nil {
		return fmt.Errorf("failed to wait for patroni to enter running state: %w", err)
	}

	primaryInstanceID, err := GetPrimaryInstanceID(ctx, patroniClient, time.Minute)
	if err != nil {
		return err
	}
	r.PrimaryInstanceID = primaryInstanceID

	if r.Spec.InstanceID != r.PrimaryInstanceID {
		err = r.updateInstanceState(ctx, rc, &InstanceUpdateOptions{State: InstanceStateAvailable})
		if err != nil {
			return r.recordError(ctx, rc, err)
		}
		// no other initialization needed on non-primary instances
		return nil
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	firstTimeSetup, err := r.isFirstTimeSetup(rc)
	if err != nil {
		return err
	}

	if r.Spec.RestoreConfig != nil && firstTimeSetup {
		err = r.renameDB(ctx, tlsCfg)
		if err != nil {
			return fmt.Errorf("failed to rename database %q: %w", r.Spec.DatabaseName, err)
		}
		err = r.dropSpock(ctx, tlsCfg)
		if err != nil {
			return fmt.Errorf("failed to drop spock: %w", err)
		}
	}

	err = r.createDB(ctx, tlsCfg)
	if err != nil {
		return fmt.Errorf("failed to create database %q: %w", r.Spec.DatabaseName, err)
	}

	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: r.ConnectionInfo.AdminDSN(r.Spec.DatabaseName),
		TLS: tlsCfg,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", r.Spec.DatabaseName, err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	enabled, err := postgres.IsSpockEnabled().Row(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to check if spock is enabled: %w", err)
	}

	if enabled {
		err = postgres.EnableRepairMode().Exec(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to enable repair mode: %w", err)
		}
	}

	err = postgres.InitializePgEdgeExtensions(
		r.Spec.NodeName,
		r.ConnectionInfo.PeerDSN(r.Spec.DatabaseName),
	).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to initialize pgedge extensions: %w", err)
	}
	roleStatements, err := postgres.CreateBuiltInRoles(postgres.BuiltinRoleOptions{
		PGVersion: r.Spec.PgEdgeVersion.PostgresVersion.String(),
		DBName:    r.Spec.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to generate built-in role statements: %w", err)
	}
	if err := roleStatements.Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to create built-in roles: %w", err)
	}

	for _, user := range r.Spec.DatabaseUsers {
		statement, err := postgres.CreateUserRole(postgres.UserRoleOptions{
			Name:       user.Username,
			Password:   user.Password,
			DBName:     r.Spec.DatabaseName,
			DBOwner:    user.DBOwner,
			Attributes: user.Attributes,
			Roles:      user.Roles,
		})
		if err != nil {
			return fmt.Errorf("failed to produce create user role statement %q: %w", user.Username, err)
		}
		if err := statement.Exec(ctx, conn); err != nil {
			return fmt.Errorf("failed to create user role %q: %w", user.Username, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	err = r.updateInstanceState(ctx, rc, &InstanceUpdateOptions{State: InstanceStateAvailable})
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	// attempt to apply it now that the database and spock extension are initialized.
	if applyErr := r.ApplySpockRepsetBackup(ctx, rc); applyErr != nil {
		_ = r.updateInstanceState(ctx, rc, &InstanceUpdateOptions{
			State: InstanceStateAvailable,
			Error: fmt.Sprintf("spock repset apply warning: %v", applyErr),
		})
	}
	return nil
}

func (r *InstanceResource) updateInstanceState(ctx context.Context, rc *resource.Context, opts *InstanceUpdateOptions) error {
	svc, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}
	opts.InstanceID = r.Spec.InstanceID
	opts.DatabaseID = r.Spec.DatabaseID
	opts.HostID = r.Spec.HostID
	opts.NodeName = r.Spec.NodeName
	err = svc.UpdateInstance(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to update instance state: %w", err)
	}

	return nil
}

func (r *InstanceResource) recordError(ctx context.Context, rc *resource.Context, cause error) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	err = r.updateInstanceState(ctx, rc, &InstanceUpdateOptions{
		State: InstanceStateFailed,
		Error: cause.Error(),
	})
	if err != nil {
		logger.Err(err).Msg("failed to persist instance error status")
	}

	return cause
}

func (r *InstanceResource) updateConnectionInfo(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}
	connInfo, err := orch.GetInstanceConnectionInfo(ctx, r.Spec.DatabaseID, r.Spec.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance connection info: %w", err)
	}
	r.ConnectionInfo = connInfo

	return nil
}

func (r *InstanceResource) patroniClient() *patroni.Client {
	return patroni.NewClient(r.ConnectionInfo.PatroniURL(), nil)
}

func (r *InstanceResource) createDB(ctx context.Context, tlsCfg *tls.Config) error {
	createDBConn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: r.ConnectionInfo.AdminDSN("postgres"),
		TLS: tlsCfg,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to 'postgres' database on instance: %w", err)
	}
	defer createDBConn.Close(ctx)

	err = postgres.CreateDatabase(r.Spec.DatabaseName).Exec(ctx, createDBConn)
	if err != nil {
		return fmt.Errorf("failed to create database %q: %w", r.Spec.DatabaseName, err)
	}

	return nil
}

func (r *InstanceResource) renameDB(ctx context.Context, tlsCfg *tls.Config) error {
	// Short circuit if the restore config doesn't include a dbname or if the
	// database name is the same.
	if r.Spec.RestoreConfig.SourceDatabaseName == "" || r.Spec.RestoreConfig.SourceDatabaseName == r.Spec.DatabaseName {
		return nil
	}

	// This operation can be flaky because of other processes connected to the
	// database. We retry it a few times to avoid failing the entire create
	// operation.
	err := utils.Retry(3, 500*time.Millisecond, func() error {
		createDBConn, err := ConnectToInstance(ctx, &ConnectionOptions{
			DSN: r.ConnectionInfo.AdminDSN("postgres"),
			TLS: tlsCfg,
		})
		if err != nil {
			return fmt.Errorf("failed to connect to 'postgres' database on instance: %w", err)
		}
		defer createDBConn.Close(ctx)

		return postgres.
			RenameDB(r.Spec.RestoreConfig.SourceDatabaseName, r.Spec.DatabaseName).
			Exec(ctx, createDBConn)
	})
	if err != nil {
		return fmt.Errorf("failed to rename database %q: %w", r.Spec.DatabaseName, err)
	}

	return nil
}

func (r *InstanceResource) dropSpock(ctx context.Context, tlsCfg *tls.Config) error {
	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: r.ConnectionInfo.AdminDSN(r.Spec.DatabaseName),
		TLS: tlsCfg,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", r.Spec.DatabaseName, err)
	}
	defer conn.Close(ctx)

	err = postgres.DropSpockAndCleanupSlots(r.Spec.DatabaseName).Exec(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to drop spock: %w", err)
	}

	return nil
}

func (r *InstanceResource) isFirstTimeSetup(rc *resource.Context) (bool, error) {
	// This instance will already exist in the state if it's been successfully
	// created before.
	_, err := resource.FromContext[*InstanceResource](rc, r.Identifier())
	if errors.Is(err, resource.ErrNotFound) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check state for previous version of this instance: %w", err)
	}

	return false, nil
}

// ApplySpockRepsetBackup applies an in-memory Spock repset backup (if present).
func (r *InstanceResource) ApplySpockRepsetBackup(ctx context.Context, rc *resource.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return fmt.Errorf("failed to get logger: %w", err)
	}

	// Connect to restored DB
	conn, err := r.Connection(ctx, rc, r.Spec.DatabaseName)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to restored database to apply spock backup")
		return fmt.Errorf("connect to restored db: %w", err)
	}
	defer conn.Close(ctx)

	// Prepare session for restore and ensure GUCs are restored afterward
	if err := postgres.SetGUCsForRestore().Exec(ctx, conn); err != nil {
		logger.Warn().Err(err).Msg("failed to set GUCs for restore (continuing)")
	}
	defer func() {
		if err := postgres.RestoreGUCsAfterRestore().Exec(ctx, conn); err != nil {
			logger.Warn().Err(err).Msg("failed to restore GUCs after restore")
		}
	}()

	// Load payload (if present)
	payload := r.getInMemorySpockPayload()

	appliedSets, appliedTables := 0, 0
	if len(payload) > 0 {
		top, err := r.parsePayload(payload)
		if err != nil {
			logger.Error().Err(err).Msg("failed to parse spock repset payload")
			return err
		}

		repSets, err := r.extract(top, "replication_sets", "replicationSets", "repsets")
		if err != nil {
			logger.Error().Err(err).Msg("failed to extract replication_sets from payload")
			return err
		}
		repSetTables, err := r.extract(top, "replication_set_tables", "replicationSetTables", "replication_set_table", "repset_tables")
		if err != nil {
			logger.Error().Err(err).Msg("failed to extract replication_set_tables from payload")
			return err
		}

		appliedSets = r.applyRepSets(ctx, conn, &logger, repSets)
		appliedTables = r.applyRepSetTables(ctx, conn, &logger, repSetTables)
	} else {
		logger.Info().Msg("no in-memory spock repset payload found; still running default-set fallback")
	}

	// Ensure default sets include public schema tables (idempotent)
	if err := postgres.RepsetAddAllTables("default", []string{"public"}).Exec(ctx, conn); err != nil {
		logger.Warn().Err(err).Msg("spock.repset_add_all_tables('default') failed")
	} else {
		logger.Info().Msg("ensured spock.repset_add_all_tables('default') applied")
	}
	if err := postgres.RepsetAddAllTables("default_insert_only", []string{"public"}).Exec(ctx, conn); err != nil {
		logger.Warn().Err(err).Msg("spock.repset_add_all_tables('default_insert_only') failed")
	} else {
		logger.Info().Msg("ensured spock.repset_add_all_tables('default_insert_only') applied")
	}

	logger.Info().Int("repsets_applied", appliedSets).Int("tables_applied", appliedTables).Msg("completed applying spock repset backup (in-memory)")
	return nil
}

func (r *InstanceResource) getInMemorySpockPayload() json.RawMessage {
	if r.SpockRepsetBackup != nil && len(r.SpockRepsetBackup.Payload) > 0 {
		return r.SpockRepsetBackup.Payload
	}
	return nil
}

func (r *InstanceResource) parsePayload(payload json.RawMessage) (map[string]json.RawMessage, error) {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal top-level payload: %w", err)
	}
	return resp, nil
}

func (r *InstanceResource) extract(top map[string]json.RawMessage, keys ...string) ([]map[string]any, error) {
	for _, k := range keys {
		if raw, ok := top[k]; ok && len(raw) > 0 {
			var arr []map[string]any
			if err := json.Unmarshal(raw, &arr); err != nil {
				return nil, fmt.Errorf("unmarshal key %q: %w", k, err)
			}
			return arr, nil
		}
	}

	return []map[string]any{}, nil
}

func (r *InstanceResource) applyRepSets(ctx context.Context, conn *pgx.Conn, logger *zerolog.Logger, repSets []map[string]any) int {
	count := 0
	for _, item := range repSets {
		name := r.getString(item, "set_name", "setName", "repsetname", "name")
		if name == "" {
			continue
		}
		// skip defaults
		if name == "default" || name == "default_insert_only" || name == "ddl_sql" {
			continue
		}
		repInsert := r.parseBool(item["replicate_insert"], true)
		repUpdate := r.parseBool(item["replicate_update"], true)
		repDelete := r.parseBool(item["replicate_delete"], true)
		repTrunc := r.parseBool(item["replicate_truncate"], true)

		if err := postgres.RepsetCreateIfNotExists(name, repInsert, repUpdate, repDelete, repTrunc).Exec(ctx, conn); err != nil {
			logger.Warn().Err(err).Str("repset", name).Msg("failed to create spock repset (continuing)")
			continue
		}
		count++
		logger.Info().Str("repset", name).Msg("ensured spock repset exists")
	}
	return count
}

func (r *InstanceResource) applyRepSetTables(ctx context.Context, conn *pgx.Conn, logger *zerolog.Logger, repSetTables []map[string]any) int {
	count := 0
	for _, item := range repSetTables {
		setName := r.getString(item, "set_name", "setName", "repsetname", "set")
		if setName == "" {
			continue
		}

		// determine relation: prefer table_name/tableName; else schema+table
		relation := r.getString(item, "table_name", "tableName")
		if relation == "" {
			schema := r.getString(item, "schema")
			tbl := r.getString(item, "table", "relname")
			if schema != "" && tbl != "" {
				relation = fmt.Sprintf("%s.%s", strings.TrimSpace(schema), strings.TrimSpace(tbl))
			}
		}
		if relation == "" {
			continue
		}

		attList := r.getStringSlice(item["set_att_list"])
		var rowFilter *string
		if rf := r.getString(item, "set_row_filter", "row_filter"); rf != "" {
			rowFilter = &rf
		}

		// try options helper first
		if len(attList) > 0 || rowFilter != nil {
			if stmtWithOpts := postgres.RepsetAddTableWithOptions(setName, relation, attList, rowFilter); stmtWithOpts != nil {
				if err := stmtWithOpts.Exec(ctx, conn); err != nil {
					logger.Warn().Err(err).Str("repset", setName).Str("relation", relation).Msg("failed to add table (with options) to spock repset (continuing)")
					continue
				}
				count++
				logger.Info().Str("repset", setName).Str("relation", relation).Msg("added table to spock repset (with options)")
				continue
			}
		}

		// fallback simple add
		if err := postgres.RepsetAddTableIfNotExists(setName, relation).Exec(ctx, conn); err != nil {
			logger.Warn().Err(err).Str("repset", setName).Str("relation", relation).Msg("failed to add table to spock repset (continuing)")
			continue
		}
		count++
		logger.Info().Str("repset", setName).Str("relation", relation).Msg("added table to spock repset")
	}
	return count
}

func (r *InstanceResource) parseBool(v any, def bool) bool {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "t" || s == "1"
	default:
		s := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", t)))
		return s == "true" || s == "t" || s == "1"
	}
}

func (r *InstanceResource) getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func (r *InstanceResource) getStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch vv := v.(type) {
	case []any:
		out := make([]string, 0, len(vv))
		for _, e := range vv {
			out = append(out, fmt.Sprintf("%v", e))
		}
		return out
	case string:
		parts := strings.Split(vv, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{fmt.Sprintf("%v", vv)}
	}
}
