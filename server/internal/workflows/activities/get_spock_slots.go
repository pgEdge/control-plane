package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GetSpockSlotsInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
}

type GetSpockSlotsOutput struct {
	Slots []postgres.SpockSlot `json:"slots"`
}

// ExecuteGetSpockSlots lists an instance's Spock replication slots. It runs
// on the instance's host because the client certificate is only valid there.
func (a *Activities) ExecuteGetSpockSlots(
	ctx workflow.Context,
	hostID string,
	input *GetSpockSlotsInput,
) workflow.Future[*GetSpockSlotsOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetSpockSlotsOutput](ctx, options, a.GetSpockSlots, input)
}

func (a *Activities) GetSpockSlots(ctx context.Context, input *GetSpockSlotsInput) (*GetSpockSlotsOutput, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "instance_id", input.InstanceID)
	logger.Debug("listing spock replication slots")

	certSvc, err := do.Invoke[*certificates.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	info, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}
	tlsCfg, err := certSvc.PostgresUserTLS(ctx, input.InstanceID, info.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}
	// Replication slots are cluster-wide, so any database will do.
	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: info.AdminDSN("postgres"),
		TLS: tlsCfg,
	})
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	slots, err := postgres.GetSpockSlots().Structs(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to query replication slots: %w", err)
	}

	return &GetSpockSlotsOutput{Slots: slots}, nil
}
