package database

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func GetPatroniClient(ctx context.Context, orch Orchestrator, databaseID, instanceID uuid.UUID) (*patroni.Client, error) {
	connInfo, err := orch.GetInstanceConnectionInfo(ctx, databaseID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance DSN: %w", err)
	}
	patroniURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", connInfo.AdminHost, connInfo.PatroniPort),
	}
	return patroni.NewClient(patroniURL, nil), nil
}

func WaitForPatroniRunning(ctx context.Context, orch Orchestrator, databaseID, instanceID uuid.UUID, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	patroniClient, err := GetPatroniClient(ctx, orch, databaseID, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get patroni client: %w", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := patroniClient.GetInstanceStatus(ctx)
			if err != nil {
				return fmt.Errorf("failed to get cluster status: %w", err)
			}
			if status.InRunningState() {
				return nil
			} else if status.InErrorState() {
				return fmt.Errorf("instance is in error state: %s", utils.FromPointer(status.State))
			}
		}
	}
}

func GetPrimaryInstanceID(ctx context.Context, orch Orchestrator, databaseID, instanceID uuid.UUID, timeout time.Duration) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	patroniClient, err := GetPatroniClient(ctx, orch, databaseID, instanceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get patroni client: %w", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		case <-ticker.C:
			status, err := patroniClient.GetClusterStatus(ctx)
			if err != nil {
				return uuid.Nil, fmt.Errorf("failed to get cluster status: %w", err)
			}
			for _, m := range status.Members {
				if m.IsLeader() && m.Name != nil {
					id, err := uuid.Parse(*m.Name)
					if err != nil {
						return uuid.Nil, fmt.Errorf("failed to parse instance ID from member name %q: %w", *m.Name, err)
					}
					return id, nil
				}
			}
		}
	}
}
