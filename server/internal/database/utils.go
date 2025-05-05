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
	patroniClient, err := GetPatroniClient(ctx, orch, databaseID, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get patroni client: %w", err)
	}

	err = utils.WithTimeout(ctx, timeout, func(ctx context.Context) error {
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status, err := patroniClient.GetInstanceStatus(ctx)
			if err != nil {
				return fmt.Errorf("failed to get cluster status: %w", err)
			}
			if status.InRunningState() {
				return nil
			} else if status.InErrorState() {
				return fmt.Errorf("instance is in error state: %s", utils.FromPointer(status.State))
			}

			// Retry after a short delay
			time.Sleep(5 * time.Second)
		}
	})
	if err != nil {
		return err
	}

	return nil
}

func GetPrimaryInstanceID(ctx context.Context, orch Orchestrator, databaseID, instanceID uuid.UUID, timeout time.Duration) (uuid.UUID, error) {
	patroniClient, err := GetPatroniClient(ctx, orch, databaseID, instanceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get patroni client: %w", err)
	}

	var primaryInstanceID uuid.UUID
	err = utils.WithTimeout(ctx, timeout, func(ctx context.Context) error {
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status, err := patroniClient.GetClusterStatus(ctx)
			if err != nil {
				return fmt.Errorf("failed to get cluster status: %w", err)
			}

			for _, m := range status.Members {
				if !m.IsLeader() {
					continue
				}
				if m.Name == nil {
					continue
				}
				id, err := uuid.Parse(*m.Name)
				if err != nil {
					return fmt.Errorf("failed to parse instance ID from member name %q: %w", *m.Name, err)
				}
				primaryInstanceID = id
				return nil
			}

			// Retry after a short delay
			time.Sleep(5 * time.Second)
		}
	})
	if err != nil {
		return uuid.Nil, err
	}

	return primaryInstanceID, nil
}
