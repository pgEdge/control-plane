package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func WaitForPatroniRunning(ctx context.Context, patroniClient *patroni.Client, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// We want some tolerance to transient connection errors.
	const maxConnectionErrors = 3
	var errCount int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := patroniClient.GetInstanceStatus(ctx)
			if err != nil {
				errCount++
				if errCount >= maxConnectionErrors {
					return fmt.Errorf("failed to get cluster status: %w", err)
				}
				continue
			}
			if status.InRunningState() {
				return nil
			} else if status.InErrorState() {
				return fmt.Errorf("instance is in error state: %s", utils.FromPointer(status.State))
			}
		}
	}
}

func GetPrimaryInstanceID(ctx context.Context, patroniClient *patroni.Client, timeout time.Duration) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
