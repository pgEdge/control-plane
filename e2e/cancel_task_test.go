//go:build e2e_test

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
	"github.com/stretchr/testify/assert"
)

func TestCancelDatabaseTask(t *testing.T) {
	testCancelDB(t)
}
func testCancelDB(t *testing.T) {
	dbID := uuid.NewString()
	host1 := fixture.HostIDs()[0]

	create_resp, err := fixture.Client.CreateDatabase(t.Context(), &controlplane.CreateDatabaseRequest{
		ID: pointerTo(controlplane.Identifier(dbID)),
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: "test_cancel",
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name:    "n1",
					HostIds: []controlplane.Identifier{controlplane.Identifier(host1)},
				},
			},
		},
	})
	creation_task := create_resp.Task
	database := create_resp.Database
	if err != nil {
		t.Logf("problem creating test db %s", err)
		return
	}
	t.Logf("successfully created cancel task test db")

	cancelation_task, err := fixture.Client.CancelDatabaseTask(t.Context(), &controlplane.CancelDatabaseTaskPayload{
		DatabaseID: database.ID,
		TaskID:     controlplane.Identifier(creation_task.TaskID),
	})
	if err != nil {
		t.Logf("cancelation failed because  %s", err)
		return
	}
	t.Logf("canceled")

	final_task, err := fixture.Client.WaitForTask(t.Context(), &controlplane.GetDatabaseTaskPayload{
		DatabaseID: database.ID,
		TaskID:     cancelation_task.TaskID,
	})
	t.Logf("waited for task")

	assert.Equal(t, final_task.Status, client.TaskStatusCanceled)

	database, _ = fixture.Client.GetDatabase(t.Context(), &controlplane.GetDatabasePayload{DatabaseID: create_resp.Database.ID})
	assert.Equal(t, database.State, client.DatabaseStateFailed)
	t.Logf("assertions passed")

	t.Cleanup(func() {
		t.Logf("cleanup began")

		if fixture.skipCleanup {
			t.Logf("skipping cleanup for database %s", dbID)
			return
		}

		t.Logf("cleaning up database %s", dbID)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		_, err := fixture.Client.DeleteDatabase(ctx,
			&controlplane.DeleteDatabasePayload{
				DatabaseID: controlplane.Identifier(dbID),
				Force:      false})

		if err != nil {
			t.Logf("failed to cleanup database %s: %s", dbID, err)
		}
	})
}
