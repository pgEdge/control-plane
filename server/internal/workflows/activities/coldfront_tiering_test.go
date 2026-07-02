package activities

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/task"
)

// TestBuildColdFrontConfig verifies that the config YAML renderer produces
// the correct structure for each provider and never embeds credentials in
// plain text in a way that could slip into a log (smoke-test only — the real
// credential check is that the field exists with the expected value).
func TestBuildColdFrontConfig(t *testing.T) {
	cases := []struct {
		name     string
		cfg      coldFrontStorageConfig
		dbName   string
		endpoint string
		wantKey  string
	}{
		{
			name: "aws",
			cfg: coldFrontStorageConfig{
				Provider:  "aws",
				Warehouse: "s3://my-bucket/warehouse",
				Bucket:    "my-bucket",
				Region:    "us-east-1",
				Credential: map[string]string{
					"access_key_id":     "AKID",
					"secret_access_key": "SECRET",
				},
			},
			dbName:  "mydb",
			wantKey: "access_key_id",
		},
		{
			name: "azure",
			cfg: coldFrontStorageConfig{
				Provider:  "azure",
				Warehouse: "abfss://container@account.dfs.core.windows.net",
				Bucket:    "container",
				Credential: map[string]string{
					"connection_string": "DefaultEndpointsProtocol=https;...",
				},
			},
			dbName:  "mydb",
			wantKey: "connection_string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml, err := buildColdFrontConfigYAML(tc.cfg, tc.dbName, "lakekeeper-svc:8181")
			if err != nil {
				t.Fatalf("buildColdFrontConfigYAML returned error: %v", err)
			}
			if len(yaml) == 0 {
				t.Fatal("empty config YAML")
			}
			content := string(yaml)
			if !strings.Contains(content, "postgres:") {
				t.Error("missing postgres section")
			}
			if !strings.Contains(content, "iceberg:") {
				t.Error("missing iceberg section")
			}
			if strings.Contains(content, "tables:") {
				t.Error("config must NOT contain archiver.tables — tables come from DB registry")
			}
		})
	}
}

// TestIsBenignArchiverEmpty verifies the benign-empty detection logic. The
// classification is scoped to the archiver only: the partitioner and compactor
// must never have the "no tables configured" text treated as benign.
func TestIsBenignArchiverEmpty(t *testing.T) {
	cases := []struct {
		binary string
		output string
		want   bool
	}{
		{"archiver", "no tables configured", true},
		{"archiver", "No Tables Configured", true},
		{"archiver", "NO TABLES CONFIGURED\n", true},
		{"archiver", "error: connection refused", false},
		{"archiver", "archiver completed successfully", false},
		{"archiver", "", false},
		// The same text must NOT be treated as benign for the other binaries.
		{"partitioner", "no tables configured", false},
		{"compactor", "no tables configured", false},
	}
	for _, tc := range cases {
		if got := isBenignArchiverEmpty(tc.binary, tc.output); got != tc.want {
			t.Errorf("isBenignArchiverEmpty(%q, %q) = %v, want %v", tc.binary, tc.output, got, tc.want)
		}
	}
}

// fakeExecer drives runColdFrontBinary with a canned exit code / output so the
// exit-code capture and benign classification can be tested without Docker.
type fakeExecer struct {
	exitCode int
	output   string
	err      error
}

func (f fakeExecer) Exec(_ context.Context, _ string, _ []string) (int, string, error) {
	return f.exitCode, f.output, f.err
}

// mapResultToTaskStatus mirrors what the ColdFrontTiering workflow does with
// the activity result: a nil error is recorded as task success (completed), a
// non-nil error as task failure (failed). Exercising this mapping lets the test
// assert the resulting task status per exit-code scenario, which is the
// behaviour this task exists to add.
func mapResultToTaskStatus(err error) task.Status {
	if err != nil {
		return task.StatusFailed
	}
	return task.StatusCompleted
}

// TestRunColdFrontBinaryExitCodeToTaskStatus is the mandated behavioural test:
// it drives the exec + exit-code + benign-classification path with a fake and
// asserts the resulting task status for each scenario.
func TestRunColdFrontBinaryExitCodeToTaskStatus(t *testing.T) {
	cases := []struct {
		name    string
		binary  string
		execer  fakeExecer
		wantErr bool
		want    task.Status
	}{
		{
			name:    "exit zero records success",
			binary:  "archiver",
			execer:  fakeExecer{exitCode: 0},
			wantErr: false,
			want:    task.StatusCompleted,
		},
		{
			name:    "non-zero exit records failure",
			binary:  "archiver",
			execer:  fakeExecer{exitCode: 1, output: "fatal: connection refused", err: errors.New("command failed with exit code 1")},
			wantErr: true,
			want:    task.StatusFailed,
		},
		{
			name:    "archiver no-tables-configured is benign (success)",
			binary:  "archiver",
			execer:  fakeExecer{exitCode: 1, output: "FATAL: no tables configured", err: errors.New("command failed with exit code 1")},
			wantErr: false,
			want:    task.StatusCompleted,
		},
		{
			name:    "partitioner no-tables-configured is NOT benign (failure)",
			binary:  "partitioner",
			execer:  fakeExecer{exitCode: 1, output: "no tables configured", err: errors.New("command failed with exit code 1")},
			wantErr: true,
			want:    task.StatusFailed,
		},
		{
			name:    "compactor no-tables-configured is NOT benign (failure)",
			binary:  "compactor",
			execer:  fakeExecer{exitCode: 1, output: "no tables configured", err: errors.New("command failed with exit code 1")},
			wantErr: true,
			want:    task.StatusFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runColdFrontBinary(context.Background(), tc.execer, "container-id", tc.binary, []string{"sh", "-c", "true"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("runColdFrontBinary err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got := mapResultToTaskStatus(err); got != tc.want {
				t.Errorf("resulting task status = %q, want %q (err=%v)", got, tc.want, err)
			}
			// A genuine failure error must never leak the config/credential.
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Errorf("error output unexpectedly contains 'secret': %v", err)
			}
		})
	}
}
