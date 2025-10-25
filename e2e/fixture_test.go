//go:build e2e_test

package e2e

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net/url"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

type TestConfigHost struct {
	ExternalIP string `yaml:"external_ip"`
	Port       int    `yaml:"port"`
	SSHCommand string `yaml:"ssh_command"`
}

type TestConfigS3 struct {
	Enabled         bool   `yaml:"enabled"`
	Bucket          string `yaml:"bucket"`
	Region          string `yaml:"region"`
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
}

func (t TestConfigS3) BackupRepository() *controlplane.BackupRepositorySpec {
	cfg := &controlplane.BackupRepositorySpec{
		Type:          client.RepositoryTypeS3,
		S3Bucket:      &t.Bucket,
		S3Region:      &t.Region,
		CustomOptions: map[string]string{},
	}
	if t.Endpoint != "" {
		cfg.S3Endpoint = &t.Endpoint
		cfg.CustomOptions["s3-uri-style"] = "path"
		cfg.CustomOptions["storage-verify-tls"] = "n"
	}
	if t.AccessKeyID != "" {
		cfg.S3Key = &t.AccessKeyID
	}
	if t.SecretAccessKey != "" {
		cfg.S3KeySecret = &t.SecretAccessKey
	}
	return cfg
}

func (t TestConfigS3) RestoreRepository() *controlplane.RestoreRepositorySpec {
	cfg := &controlplane.RestoreRepositorySpec{
		Type:          client.RepositoryTypeS3,
		S3Bucket:      &t.Bucket,
		S3Region:      &t.Region,
		CustomOptions: map[string]string{},
	}
	if t.Endpoint != "" {
		cfg.S3Endpoint = &t.Endpoint
		cfg.CustomOptions["s3-uri-style"] = "path"
		cfg.CustomOptions["storage-verify-tls"] = "n"
	}
	if t.AccessKeyID != "" {
		cfg.S3Key = &t.AccessKeyID
	}
	if t.SecretAccessKey != "" {
		cfg.S3KeySecret = &t.SecretAccessKey
	}
	return cfg
}

type TestConfig struct {
	Hosts map[string]TestConfigHost `yaml:"hosts"`
	S3    TestConfigS3              `yaml:"s3"`
}

func DefaultTestConfig() TestConfig {
	return TestConfig{
		// Matches our local docker-compose configuration
		Hosts: map[string]TestConfigHost{
			"host-1": {
				ExternalIP: "127.0.0.1",
				Port:       3000,
			},
			"host-2": {
				ExternalIP: "127.0.0.1",
				Port:       3001,
			},
			"host-3": {
				ExternalIP: "127.0.0.1",
				Port:       3002,
			},
			"host-4": {
				ExternalIP: "127.0.0.1",
				Port:       3003,
			},
			"host-5": {
				ExternalIP: "127.0.0.1",
				Port:       3004,
			},
			"host-6": {
				ExternalIP: "127.0.0.1",
				Port:       3005,
			},
		},
	}
}

type TestFixture struct {
	Client      *client.MultiServerClient
	config      TestConfig
	skipCleanup bool
	debug       bool
	debugDir    string
}

func NewTestFixture(ctx context.Context, config TestConfig, skipCleanup bool, debug bool, debugDir string) (*TestFixture, error) {
	servers := make([]client.ServerConfig, 0, len(config.Hosts))
	for host, cfg := range config.Hosts {
		server := client.NewHTTPServerConfig(host, &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%d", cfg.ExternalIP, cfg.Port),
		})
		servers = append(servers, server)
	}

	cli, err := client.NewMultiServerClient(servers...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	log.Print("initializing cluster")

	// Ensure that the cluster is initialized
	_, err = cli.InitCluster(ctx, &api.InitClusterRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cluster: %w", err)
	}

	log.Print("cluster initialized")

	return &TestFixture{
		Client:      cli,
		config:      config,
		skipCleanup: skipCleanup,
		debug:       debug,
		debugDir:    debugDir,
	}, nil
}

func (f *TestFixture) SkipCleanup() bool {
	return f.skipCleanup
}

func (f *TestFixture) HostIDs() []string {
	return slices.Sorted(maps.Keys(f.config.Hosts))
}

func (f *TestFixture) NewDatabaseFixture(ctx context.Context, t testing.TB, req *controlplane.CreateDatabaseRequest) *DatabaseFixture {
	var db *DatabaseFixture

	t.Cleanup(func() {
		if db == nil || db.Database == nil {
			return
		}

		if t.Failed() && f.debug {
			debugWriteDatabaseInfo(t, f.debugDir, string(db.ID))
		}

		if f.skipCleanup {
			t.Logf("skipping cleanup for database %s", db.ID)
			return
		}

		t.Logf("cleaning up database %s", db.ID)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		err := db.EnsureDelete(ctx)
		if err != nil {
			t.Logf("failed to cleanup database %s: %s", db.ID, err)
		}
	})

	var err error
	db, err = NewDatabaseFixture(ctx, f.config, f.Client, req, f.debug, f.debugDir)
	if err != nil {
		t.Fatal(err)
		return nil
	}

	return db
}

func (f *TestFixture) RunCmdOnHost(hostID string, c string) (string, error) {
	host, ok := f.config.Hosts[hostID]
	if !ok {
		return "", fmt.Errorf("host %s not found", hostID)
	}

	var cmd *exec.Cmd
	if host.SSHCommand != "" {
		// Operate as root on remote hosts
		c = "sudo " + c
		parts := strings.Split(host.SSHCommand, " ")
		parts = append(parts, c)
		cmd = exec.Command(parts[0], parts[1:]...)
	} else {
		// On MacOS and Windows, the docker host runs inside a VM, and file
		// ownership changes aren't reflected on the host system. This means
		// that the current user will have permission to remove files created
		// by the database container. On linux, the ownership changes are
		// reflected on the host, so we need to use 'sudo' to remove the files.
		if runtime.GOOS == "linux" {
			c = "sudo " + c
		}
		cmd = exec.Command("sh", "-c", c)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to execute command `%s`, err: %w, output: %s", cmd, err, string(out))
	}

	return string(out), nil
}

func (f *TestFixture) TempDir(hostID string, t testing.TB) string {
	dir := fmt.Sprintf("/tmp/control-plane-e2e-%s", uuid.New())

	_, err := f.RunCmdOnHost(hostID, "mkdir -m 0777 -p "+dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if f.skipCleanup {
			t.Logf("skipping cleanup for temp dir %s on host %s", dir, hostID)
			return
		}

		t.Logf("cleaning up temp dir %s on host %s", dir, hostID)

		if _, err := f.RunCmdOnHost(hostID, "rm -rf "+dir); err != nil {
			t.Logf("failed to cleanup temp dir %s on host %s: %s", dir, hostID, err)
		}
	})

	return dir
}

func (f *TestFixture) S3Enabled() bool {
	return f.config.S3.Enabled
}

func (f *TestFixture) S3BackupRepository() *controlplane.BackupRepositorySpec {
	return f.config.S3.BackupRepository()
}

func (f *TestFixture) S3RestoreRepository() *controlplane.RestoreRepositorySpec {
	return f.config.S3.RestoreRepository()
}

func (f *TestFixture) LatestPosixBackup(t testing.TB, hostID, tmpDir, databaseID string) string {
	// TODO: Replace once we add API support for listing backups.
	cmd := fmt.Sprintf("ls %s/databases/%s/n1/backup/db | grep %d", tmpDir, databaseID, time.Now().Year())
	out, err := f.RunCmdOnHost(hostID, cmd)
	if err != nil {
		t.Fatal(err)
	}
	setNames := strings.Fields(out)
	if len(setNames) == 0 {
		t.Fatalf("no backup sets found from command '%s'", cmd)
	}
	slices.Sort(setNames)

	return setNames[len(setNames)-1]
}

func (f *TestFixture) LatestS3Backup(t testing.TB, hostID, databaseID string) string {
	// TODO: Replace once we add API support for listing backups.
	cmd := fmt.Sprintf("aws s3 ls s3://%s/databases/%s/n1/backup/db/%d | awk '{ print $2 }'", f.config.S3.Bucket, databaseID, time.Now().Year())
	out, err := f.RunCmdOnHost(hostID, cmd)
	if err != nil {
		t.Fatal(err)
	}
	setNames := strings.Fields(out)
	if len(setNames) == 0 {
		t.Fatalf("no backup sets found from command '%s'", cmd)
	}
	slices.Sort(setNames)

	return strings.TrimSuffix(setNames[len(setNames)-1], "/")
}

func (f *TestFixture) CleanupS3Backups(t testing.TB, hostID, databaseID string) {
	if f.skipCleanup {
		t.Logf("skipping cleanup of s3 backups for database %s", databaseID)
		return
	}

	t.Logf("cleaning up s3 backups for database %s", databaseID)

	cmd := fmt.Sprintf("aws s3 rm --recursive s3://%s/databases/%s", f.config.S3.Bucket, databaseID)
	_, err := f.RunCmdOnHost(hostID, cmd)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *TestFixture) APIBaseURL() string {
	ids := f.HostIDs()
	if len(ids) == 0 {
		return ""
	}
	h := f.config.Hosts[ids[0]]
	return fmt.Sprintf("http://%s:%d", h.ExternalIP, h.Port)
}

func tLog(t testing.TB, args ...any) {
	t.Helper()

	prefix := fmt.Sprintf("[%s]", t.Name())
	all := append([]any{prefix}, args...)
	t.Log(all...)
}

func tLogf(t testing.TB, format string, args ...any) {
	t.Helper()

	prefix := fmt.Sprintf("[%s] ", t.Name())
	format = prefix + format
	t.Logf(format, args...)
}
