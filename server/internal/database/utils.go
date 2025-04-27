package database

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/patroni"
)

func GetPrimaryInstanceID(ctx context.Context, orch Orchestrator, databaseID, instanceID uuid.UUID) (uuid.UUID, error) {
	connInfo, err := orch.GetInstanceConnectionInfo(ctx, databaseID, instanceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get instance DSN: %w", err)
	}
	patroniURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", connInfo.AdminHost, connInfo.PatroniPort),
	}
	patroniClient := patroni.NewClient(patroniURL, nil)

	status, err := patroniClient.GetClusterStatus(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get cluster status: %w", err)
	}

	var primaryInstanceID uuid.UUID
	for _, m := range status.Members {
		if !m.IsLeader() {
			continue
		}
		if m.Name == nil {
			continue
		}
		id, err := uuid.Parse(*m.Name)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to parse instance ID from member name %q: %w", *m.Name, err)
		}
		primaryInstanceID = id
		break
	}

	return primaryInstanceID, nil
}
