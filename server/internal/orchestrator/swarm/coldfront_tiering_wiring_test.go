package swarm

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/scheduler"
)

// makeLakekeeperSpecWithStorage extends makeLakekeeperSpec with the storage
// config required to produce tiering schedule resources.
func makeLakekeeperSpecWithStorage() *database.ServiceInstanceSpec {
	cfg := map[string]any{
		"catalog_db_url":    "postgres://lakekeeper:secret@pg:5432/lk?sslmode=disable",
		"pg_encryption_key": "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==",
		"provider":          "aws",
		"warehouse":         "s3://my-bucket/warehouse",
		"bucket":            "my-bucket",
		"region":            "us-east-1",
		"credential":        `{"access_key_id":"AKID","secret_access_key":"SECRET"}`,
	}
	return &database.ServiceInstanceSpec{
		ServiceInstanceID: "inst-lakekeeper-1",
		ServiceSpec: &database.ServiceSpec{
			ServiceID:   "lakekeeper-svc",
			ServiceType: "lakekeeper",
			Version:     "0.9.0",
			Config:      cfg,
		},
		DatabaseID:   "db-1",
		DatabaseName: "testdb",
		HostID:       "host-1",
		NodeName:     "n1",
	}
}

// TestGenerateLakekeeperInstanceResources_TieringSchedules verifies that when
// a lakekeeper service has storage config (provider + credential), the
// generated resource graph includes ScheduledJobResource entries for the
// archiver, partitioner, and compactor tiering jobs.
func TestGenerateLakekeeperInstanceResources_TieringSchedules(t *testing.T) {
	o := newLakekeeperTestOrchestrator()
	spec := makeLakekeeperSpecWithStorage()

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	// Collect all ScheduledJobResource identifiers from the result.
	var scheduledIDs []string
	for _, rd := range result.Resources {
		if rd.Identifier.Type == scheduler.ResourceTypeScheduledJob {
			scheduledIDs = append(scheduledIDs, rd.Identifier.ID)
		}
	}

	wantWorkflows := []string{
		scheduler.WorkflowColdFrontArchive,
		scheduler.WorkflowColdFrontPartition,
		scheduler.WorkflowColdFrontCompact,
	}

	// Decode each ScheduledJobResource and verify all three workflow types are present.
	foundWorkflows := map[string]bool{}
	for _, rd := range result.Resources {
		if rd.Identifier.Type != scheduler.ResourceTypeScheduledJob {
			continue
		}
		job, decErr := resource.ToResource[*scheduler.ScheduledJobResource](rd)
		if decErr != nil {
			t.Fatalf("failed to decode ScheduledJobResource %q: %v", rd.Identifier.ID, decErr)
		}
		foundWorkflows[job.Workflow] = true
	}

	for _, wf := range wantWorkflows {
		if !foundWorkflows[wf] {
			t.Errorf("missing scheduled job for workflow %q; found: %v", wf, scheduledIDs)
		}
	}
}

// TestGenerateLakekeeperInstanceResources_TieringSchedules_NoStorage verifies
// that when no storage config is present (provider absent), no tiering
// schedule resources are generated rather than returning an error.
func TestGenerateLakekeeperInstanceResources_TieringSchedules_NoStorage(t *testing.T) {
	o := newLakekeeperTestOrchestrator()
	spec := makeLakekeeperSpec(
		"postgres://lakekeeper:secret@pg:5432/lk?sslmode=disable",
		"dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdA==",
	)

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	for _, rd := range result.Resources {
		if rd.Identifier.Type == scheduler.ResourceTypeScheduledJob {
			t.Errorf("expected no ScheduledJobResource when provider absent, got %q", rd.Identifier.ID)
		}
	}
}
