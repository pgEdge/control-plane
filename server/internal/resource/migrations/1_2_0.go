package migrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/alessio/shellescape"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations/schemas/v1_1_0"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations/schemas/v1_2_0"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.StateMigration = (*Version_1_2_0)(nil)

// Version_1_2_0 migrates the swarm orchestrator to use the common orchestrator
// resources.
type Version_1_2_0 struct{}

func (v *Version_1_2_0) Version() *ds.Version {
	return resource.StateVersion_1_2_0
}

func (v *Version_1_2_0) Run(databaseID string, state *resource.State) error {
	err := errors.Join(
		v.migrateEtcdCreds(state),
		v.migratePatroniCluster(state),
		v.migratePatroniMember(state),
		v.migratePgBackRestConfig(state),
		v.migratePgBackRestStanza(databaseID, state),
		v.migratePostgresCerts(state),
		v.migratePatroniConfig(state),
		v.migrateScheduledJob(state),
	)
	v.updateDependencies(state)
	return err
}

func (v *Version_1_2_0) updateDependencies(state *resource.State) {
	// This is a catch-all to update dependencies for the removed resources
	// types.
	for _, byID := range state.Resources {
		for _, data := range byID {
			for i, dep := range data.Dependencies {
				data.Dependencies[i] = resource.Identifier{
					ID:   dep.ID,
					Type: v.transformType(dep.Type),
				}
			}
		}
	}
}

func (v *Version_1_2_0) migrateEtcdCreds(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypeEtcdCreds]
	if !ok {
		return nil
	}
	// Deleting from a map while iterating is safe, but adding to it is
	// unpredictable. We'll delete old resources as we go and then add  new ones
	// back at the end.
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.EtcdCreds
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypeEtcdCreds, err)
		}
		new := v1_2_0.EtcdCreds{
			InstanceID: old.InstanceID,
			DatabaseID: old.DatabaseID,
			HostID:     old.HostID,
			NodeName:   old.NodeName,
			ParentID:   old.ParentID,
			OwnerUID:   old.OwnerUID,
			OwnerGID:   old.OwnerGID,
			Username:   old.Username,
			Password:   old.Password,
			CaCert:     old.CaCert,
			ClientCert: old.ClientCert,
			ClientKey:  old.ClientKey,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypeEtcdCreds, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_2_0.EtcdCredsIdentifier(new.InstanceID),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_2_0.DirResourceIdentifier(new.ParentID),
			},
			Executor:        data.Executor,
			ResourceVersion: "1",
			NeedsRecreate:   data.NeedsRecreate,
			// client_cert and client_key were misnamed as server_cert and
			// server_key in the old swarm resource.
			DiffIgnore: []string{
				"/username",
				"/password",
				"/ca_cert",
				"/client_cert",
				"/client_key",
			},
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypeEtcdCreds,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migratePatroniCluster(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypePatroniCluster]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.PatroniCluster
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypePatroniCluster, err)
		}
		new := v1_2_0.PatroniCluster{
			DatabaseID: old.DatabaseID,
			NodeName:   old.NodeName,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypePatroniCluster, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier:       v1_2_0.PatroniClusterResourceIdentifier(new.NodeName),
			Attributes:       attrs,
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypePatroniCluster,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migratePatroniMember(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypePatroniMember]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.PatroniMember
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypePatroniMember, err)
		}
		new := v1_2_0.PatroniMember{
			DatabaseID: old.DatabaseID,
			NodeName:   old.NodeName,
			InstanceID: old.InstanceID,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypePatroniMember, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_2_0.PatroniMemberResourceIdentifier(new.InstanceID),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_2_0.PatroniClusterResourceIdentifier(new.NodeName),
			},
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypePatroniMember,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migratePgBackRestConfig(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypePgBackRestConfig]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.PgBackRestConfig
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypePgBackRestConfig, err)
		}
		paths := v.swarmInstancePaths()
		new := v1_2_0.PgBackRestConfig{
			InstanceID:   old.InstanceID,
			HostID:       old.HostID,
			DatabaseID:   old.DatabaseID,
			NodeName:     old.NodeName,
			Repositories: old.Repositories,
			ParentID:     old.ParentID,
			Type:         old.Type,
			OwnerUID:     old.OwnerUID,
			OwnerGID:     old.OwnerGID,
			Port:         5432,
			Paths: struct {
				Instance struct {
					BaseDir string `json:"base_dir"`
				} `json:"instance"`
				Host struct {
					BaseDir string `json:"base_dir"`
				} `json:"host"`
				PgBackRestPath string `json:"pg_backrest_path"`
				PatroniPath    string `json:"patroni_path"`
			}{
				Instance: struct {
					BaseDir string `json:"base_dir"`
				}{
					BaseDir: paths.Instance.BaseDir,
				},
				PgBackRestPath: paths.PgBackRestPath,
				PatroniPath:    paths.PatroniPath,
			},
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypePgBackRestConfig, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_2_0.PgBackRestConfigIdentifier(new.InstanceID, pgbackrest.ConfigType(new.Type)),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_2_0.DirResourceIdentifier(new.ParentID),
			},
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypePgBackRestConfig,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migrateScheduledJob(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypeScheduledJob]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.ScheduledJobResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypeScheduledJob, err)
		}
		deps := make([]struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}, len(old.DependsOn))
		for i, dep := range old.DependsOn {
			deps[i] = struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}{
				ID:   dep.ID,
				Type: v.transformTypeString(dep.Type),
			}
		}
		new := v1_2_0.ScheduledJobResource{
			ID:        old.ID,
			CronExpr:  old.CronExpr,
			Workflow:  old.Workflow,
			Args:      old.Args,
			DependsOn: deps,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypeScheduledJob, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier:       v1_2_0.ScheduledJobResourceIdentifier(old.ID),
			Attributes:       attrs,
			Dependencies:     data.Dependencies, // these will get updated by updateDependencies
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypeScheduledJob,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migratePgBackRestStanza(databaseID string, state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypePgBackRestStanza]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.PgBackRestStanza
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypePgBackRestStanza, err)
		}
		new := v1_2_0.PgBackRestStanza{
			DatabaseID: databaseID,
			NodeName:   old.NodeName,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypePgBackRestStanza, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_2_0.PgBackRestStanzaIdentifier(new.NodeName),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_2_0.NodeResourceIdentifier(new.NodeName),
			},
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypePgBackRestStanza,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migratePostgresCerts(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypePostgresCerts]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.PostgresCerts
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypePostgresCerts, err)
		}
		new := v1_2_0.PostgresCerts{
			InstanceID:        old.InstanceID,
			HostID:            old.HostID,
			InstanceAddresses: old.InstanceAddresses,
			ParentID:          old.ParentID,
			OwnerUID:          old.OwnerUID,
			OwnerGID:          old.OwnerGID,
			CaCert:            old.CaCert,
			ServerCert:        old.ServerCert,
			ServerKey:         old.ServerKey,
			SuperuserCert:     old.SuperuserCert,
			SuperuserKey:      old.SuperuserKey,
			ReplicationCert:   old.ReplicationCert,
			ReplicationKey:    old.ReplicationKey,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypePostgresCerts, err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_2_0.PostgresCertsIdentifier(new.InstanceID),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_2_0.DirResourceIdentifier(new.ParentID),
			},
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypePostgresCerts,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) migratePatroniConfig(state *resource.State) error {
	resources, ok := state.Resources[v1_1_0.ResourceTypePatroniConfig]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v1_1_0.PatroniConfig
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal %s resource: %w", v1_1_0.ResourceTypePatroniConfig, err)
		}
		if old.Spec == nil {
			return fmt.Errorf("old patroni config '%s' is missing a spec", oldID)
		}
		paths := database.InstancePaths{
			// We can't easily get the host paths here. Luckily, they're
			// irrelevant for this migration.
			Instance:       database.Paths{BaseDir: "/opt/pgedge"},
			PgBackRestPath: "/usr/bin/pgbackrest",
			PatroniPath:    "/usr/local/bin/patroni",
		}
		var archiveCommand, restoreCommand string
		if old.Spec.BackupConfig != nil {
			archiveCommand = paths.PgBackRestBackupCmd("archive-push", `"%p"`).String()
		}
		if old.Spec.RestoreConfig != nil {
			if old.Spec.InPlaceRestore {
				restoreCommand = strings.Join(paths.InstanceMvRestoreToDataCmd(), " ")
			} else {
				restoreOptions := utils.BuildOptionArgs(old.Spec.RestoreConfig.RestoreOptions)
				for i, o := range restoreOptions {
					restoreOptions[i] = shellescape.Quote(o)
				}
				restoreCommand = paths.PgBackRestRestoreCmd("restore", restoreOptions...).String()
			}
		}
		cpus := old.Spec.CPUs
		if cpus == 0 {
			cpus = old.HostCPUs
		}
		memoryBytes := old.Spec.MemoryBytes
		if memoryBytes == 0 {
			memoryBytes = old.HostMemoryBytes
		}
		var bridgeNetworkInfo *struct {
			Name    string       `json:"name"`
			ID      string       `json:"id"`
			Subnet  netip.Prefix `json:"subnet"`
			Gateway netip.Addr   `json:"gateway"`
		}
		if old.BridgeNetworkInfo != nil {
			bridgeNetworkInfo = &struct {
				Name    string       `json:"name"`
				ID      string       `json:"id"`
				Subnet  netip.Prefix `json:"subnet"`
				Gateway netip.Addr   `json:"gateway"`
			}{
				Name:    old.BridgeNetworkInfo.Name,
				ID:      old.BridgeNetworkInfo.ID,
				Subnet:  old.BridgeNetworkInfo.Subnet,
				Gateway: old.BridgeNetworkInfo.Gateway,
			}
		}
		new := v1_2_0.PatroniConfig{
			DatabaseID:          old.Spec.DatabaseID,
			BridgeNetworkInfo:   bridgeNetworkInfo,
			DatabaseNetworkName: old.DatabaseNetworkName,
			Base: &struct {
				InstanceID string `json:"instance_id"`
				HostID     string `json:"host_id"`
				NodeName   string `json:"node_name"`
				Generator  *struct {
					ArchiveCommand         string         `json:"archive_command,omitempty"`
					ClusterSize            int            `json:"cluster_size"`
					CPUs                   float64        `json:"cpus,omitempty"`
					DatabaseID             string         `json:"database_id"`
					DataDir                string         `json:"data_dir"`
					EtcdCertsDir           string         `json:"etcd_certs_dir"`
					FQDN                   string         `json:"fqdn"`
					InstanceID             string         `json:"instance_id"`
					LogType                string         `json:"log_type"`
					MemoryBytes            uint64         `json:"memory_bytes,omitempty"`
					NodeName               string         `json:"node_name"`
					NodeOrdinal            int            `json:"node_ordinal"`
					NodeSize               int            `json:"node_size"`
					OrchestratorParameters map[string]any `json:"orchestrator_parameters,omitempty"`
					PatroniAllowlist       []string       `json:"patroni_allowlist"`
					PatroniPort            int            `json:"patroni_port"`
					PgHbaConf              []string       `json:"pg_hba_conf,omitempty"`
					PgIdentConf            []string       `json:"pg_ident_conf,omitempty"`
					PostgresCertsDir       string         `json:"postgres_certs_dir"`
					PostgresPort           int            `json:"postgres_port"`
					RestoreCommand         string         `json:"restore_command"`
					SpecParameters         map[string]any `json:"spec_parameters,omitempty"`
					TenantID               *string        `json:"tenant_id,omitempty"`
				} `json:"generator"`
				ParentID string `json:"parent_id"`
				OwnerUID int    `json:"owner_uid"`
				OwnerGID int    `json:"owner_gid"`
			}{
				InstanceID: old.Spec.InstanceID,
				HostID:     old.Spec.HostID,
				NodeName:   old.Spec.NodeName,
				ParentID:   old.ParentID,
				OwnerUID:   old.OwnerUID,
				OwnerGID:   old.OwnerGID,
				Generator: &struct {
					ArchiveCommand         string         `json:"archive_command,omitempty"`
					ClusterSize            int            `json:"cluster_size"`
					CPUs                   float64        `json:"cpus,omitempty"`
					DatabaseID             string         `json:"database_id"`
					DataDir                string         `json:"data_dir"`
					EtcdCertsDir           string         `json:"etcd_certs_dir"`
					FQDN                   string         `json:"fqdn"`
					InstanceID             string         `json:"instance_id"`
					LogType                string         `json:"log_type"`
					MemoryBytes            uint64         `json:"memory_bytes,omitempty"`
					NodeName               string         `json:"node_name"`
					NodeOrdinal            int            `json:"node_ordinal"`
					NodeSize               int            `json:"node_size"`
					OrchestratorParameters map[string]any `json:"orchestrator_parameters,omitempty"`
					PatroniAllowlist       []string       `json:"patroni_allowlist"`
					PatroniPort            int            `json:"patroni_port"`
					PgHbaConf              []string       `json:"pg_hba_conf,omitempty"`
					PgIdentConf            []string       `json:"pg_ident_conf,omitempty"`
					PostgresCertsDir       string         `json:"postgres_certs_dir"`
					PostgresPort           int            `json:"postgres_port"`
					RestoreCommand         string         `json:"restore_command"`
					SpecParameters         map[string]any `json:"spec_parameters,omitempty"`
					TenantID               *string        `json:"tenant_id,omitempty"`
				}{
					ArchiveCommand:   archiveCommand,
					ClusterSize:      old.Spec.ClusterSize,
					CPUs:             cpus,
					DatabaseID:       old.Spec.DatabaseID,
					DataDir:          paths.Instance.PgData(),
					EtcdCertsDir:     paths.Instance.EtcdCertificates(),
					FQDN:             old.InstanceHostname,
					InstanceID:       old.Spec.InstanceID,
					LogType:          "json", // Swarm always uses JSON logging
					MemoryBytes:      memoryBytes,
					NodeName:         old.Spec.NodeName,
					NodeOrdinal:      old.Spec.NodeOrdinal,
					NodeSize:         old.Spec.NodeSize,
					PatroniPort:      8888,
					PgHbaConf:        old.Spec.PgHbaConf,
					PgIdentConf:      old.Spec.PgIdentConf,
					PostgresCertsDir: paths.Instance.PostgresCertificates(),
					PostgresPort:     5432,
					RestoreCommand:   restoreCommand,
					SpecParameters:   old.Spec.PostgreSQLConf,
					TenantID:         old.Spec.TenantID,
				},
			},
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal %s resource: %w", v1_2_0.ResourceTypePatroniConfig, err)
		}
		var pgBackRestDeps []resource.Identifier
		if new.Base.Generator.ArchiveCommand != "" {
			pgBackRestDeps = append(pgBackRestDeps, v1_2_0.PgBackRestConfigIdentifier(new.Base.InstanceID, pgbackrest.ConfigTypeBackup))
		}
		if new.Base.Generator.RestoreCommand != "" {
			pgBackRestDeps = append(pgBackRestDeps, v1_2_0.PgBackRestConfigIdentifier(new.Base.InstanceID, pgbackrest.ConfigTypeRestore))
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_2_0.PatroniConfigIdentifier(new.Base.InstanceID),
			Attributes: attrs,
			Dependencies: slices.Concat(
				[]resource.Identifier{
					v1_2_0.DirResourceIdentifier(new.Base.ParentID),
					v1_2_0.EtcdCredsIdentifier(new.Base.InstanceID),
					v1_2_0.PatroniMemberResourceIdentifier(new.Base.InstanceID),
					v1_2_0.PatroniClusterResourceIdentifier(new.Base.NodeName),
					v1_2_0.NetworkResourceIdentifier(new.DatabaseNetworkName),
				},
				pgBackRestDeps,
			),
			Executor:         data.Executor,
			ResourceVersion:  "1",
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		state.RemoveByIdentifier(resource.Identifier{
			Type: v1_1_0.ResourceTypePatroniConfig,
			ID:   oldID,
		})
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_2_0) swarmInstancePaths() database.InstancePaths {
	return database.InstancePaths{
		// We can't easily get the host paths here. Luckily, they're
		// irrelevant for this migration.
		Instance:       database.Paths{BaseDir: "/opt/pgedge"},
		PgBackRestPath: "/usr/bin/pgbackrest",
		PatroniPath:    "/usr/local/bin/patroni",
	}
}

func (v *Version_1_2_0) transformType(old resource.Type) resource.Type {
	var new resource.Type
	switch old {
	case v1_1_0.ResourceTypeEtcdCreds:
		new = v1_2_0.ResourceTypeEtcdCreds
	case v1_1_0.ResourceTypePatroniCluster:
		new = v1_2_0.ResourceTypePatroniCluster
	case v1_1_0.ResourceTypePatroniMember:
		new = v1_2_0.ResourceTypePatroniMember
	case v1_1_0.ResourceTypePgBackRestConfig:
		new = v1_2_0.ResourceTypePgBackRestConfig
	case v1_1_0.ResourceTypePgBackRestStanza:
		new = v1_2_0.ResourceTypePgBackRestStanza
	case v1_1_0.ResourceTypePostgresCerts:
		new = v1_2_0.ResourceTypePostgresCerts
	default:
		new = old
	}
	return new
}

func (v *Version_1_2_0) transformTypeString(old string) string {
	return string(v.transformType(resource.Type(old)))
}
