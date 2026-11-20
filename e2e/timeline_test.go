//go:build e2e_test

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

// promotionWindow is how long a stale instance is watched to confirm that
// Patroni won't promote it. The newer instance releases the leader key when it
// stops cleanly, so without check_timeline the stale instance would win the
// leader race on its first HA loop. This is well past Patroni's 30 second TTL
// and 10 second loop_wait.
const promotionWindow = 45 * time.Second

// timelineScenario is a node with two instances where the original leader has
// been left behind on an older timeline. It reproduces the sequence of events
// from a double failure:
//
//  1. The original leader stops and the replica promotes to a new timeline.
//  2. The promoted instance accepts writes and then stops.
//  3. The original leader starts alone, still on the old timeline.
type timelineScenario struct {
	db *DatabaseFixture
	// original is the instance that was the leader when the database was
	// created. It ends the scenario as a stale replica.
	original     *controlplane.Instance
	originalHost *controlplane.Host
	// promoted is the instance that promotes when the original stops. It ends
	// the scenario stopped.
	promoted     *controlplane.Instance
	promotedHost *controlplane.Host
	// timeline is the timeline that promoted moved to when it was promoted.
	timeline int
}

func newTimelineScenario(
	ctx context.Context,
	t *testing.T,
	dbName string,
	backupConfig *controlplane.BackupConfigSpec,
) *timelineScenario {
	t.Helper()

	hostIDs := fixture.HostIDs()
	require.GreaterOrEqual(t, len(hostIDs), 2, "fixture must provide at least 2 hosts")

	tLog(t, "creating database")
	db := fixture.NewDatabaseFixture(ctx, t, &controlplane.CreateDatabaseRequest{
		Spec: &controlplane.DatabaseSpec{
			DatabaseName: dbName,
			DatabaseUsers: []*controlplane.DatabaseUserSpec{
				{
					Username:   "admin",
					Password:   pointerTo("password"),
					DbOwner:    pointerTo(true),
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			},
			Port:        pointerTo(0),
			PatroniPort: pointerTo(0),
			Nodes: []*controlplane.DatabaseNodeSpec{
				{
					Name: "n1",
					HostIds: []controlplane.Identifier{
						controlplane.Identifier(hostIDs[0]),
						controlplane.Identifier(hostIDs[1]),
					},
				},
			},
			BackupConfig: backupConfig,
		},
	})

	s := &timelineScenario{db: db}

	tLog(t, "identifying the leader and the replica")
	require.True(t, waitFor(func() bool {
		s.original, s.promoted = nil, nil
		for inst := range db.GetInstances(WithNode("n1")) {
			inRecovery, err := queryInstance[bool](ctx, db, inst, "SELECT pg_is_in_recovery()")
			if err != nil {
				return false
			}
			if inRecovery {
				s.promoted = inst
			} else {
				s.original = inst
			}
		}
		return s.original != nil && s.promoted != nil
	}, 2*time.Minute), "node should have one leader and one replica")
	s.originalHost = getHost(ctx, t, s.original.HostID)
	s.promotedHost = getHost(ctx, t, s.promoted.HostID)

	tLog(t, "writing a row on the original leader")
	require.NoError(t, execInstance(ctx, db, s.original,
		"CREATE TABLE timeline_test (id INT PRIMARY KEY); INSERT INTO timeline_test VALUES (1)"))
	requireEventuallyHasRow(ctx, t, db, s.promoted, 1)

	tLog(t, "stopping the original leader so that the replica promotes")
	require.NoError(t, db.StopInstance(ctx, StopInstanceOptions{InstanceID: s.original.ID}))
	require.True(t, waitFor(func() bool {
		inRecovery, err := queryInstance[bool](ctx, db, s.promoted, "SELECT pg_is_in_recovery()")
		return err == nil && !inRecovery
	}, 2*time.Minute), "replica should promote after the leader stops")

	timeline, err := queryInstance[int](ctx, db, s.promoted, currentTimelineQuery)
	require.NoError(t, err)
	s.timeline = timeline
	tLogf(t, "replica promoted to timeline %d", s.timeline)

	tLog(t, "writing a row on the promoted instance")
	require.NoError(t, execInstance(ctx, db, s.promoted, "INSERT INTO timeline_test VALUES (2)"))

	// check_timeline compares against the history that the leader records in
	// etcd, so it can only protect against this failure once that's happened.
	tLog(t, "waiting for Patroni to record the new timeline")
	require.True(t, waitFor(func() bool {
		timelines, err := patroniHistory(s.promotedHost, s.promoted)
		if err != nil {
			tLogf(t, "failed to get Patroni history: %s", err)
			return false
		}
		// Each history entry is the timeline that the cluster switched away
		// from.
		for _, tl := range timelines {
			if tl == s.timeline-1 {
				return true
			}
		}
		return false
	}, time.Minute), "Patroni should record the switch to timeline %d", s.timeline)

	return s
}

// startStaleOriginal stops the promoted instance and starts the original one
// alone. It asserts that the original instance comes back as a replica and
// isn't promoted, since its timeline is behind the cluster's.
func (s *timelineScenario) startStaleOriginal(ctx context.Context, t *testing.T) {
	t.Helper()

	// The database isn't available while the node has no leader, so these
	// operations are forced.
	tLog(t, "stopping the promoted instance")
	require.NoError(t, s.db.StopInstance(ctx, StopInstanceOptions{
		InstanceID: s.promoted.ID,
		Force:      true,
	}))

	tLog(t, "starting the original instance alone")
	require.NoError(t, s.db.StartInstance(ctx, StartInstanceOptions{
		InstanceID: s.original.ID,
		Force:      true,
	}))
	require.True(t, waitFor(func() bool {
		inRecovery, err := queryInstance[bool](ctx, s.db, s.original, "SELECT pg_is_in_recovery()")
		return err == nil && inRecovery
	}, 2*time.Minute), "original instance should start as a replica")

	tLog(t, "confirming that the original instance isn't promoted")
	assert.Never(t, func() bool {
		inRecovery, err := queryInstance[bool](ctx, s.db, s.original, "SELECT pg_is_in_recovery()")
		return err == nil && !inRecovery
	}, promotionWindow, 5*time.Second, "an instance on an older timeline should not be promoted")
}

func TestCheckTimelinePreventsStalePromotion(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	s := newTimelineScenario(ctx, t, "check_timeline", nil)
	s.startStaleOriginal(ctx, t)

	tLog(t, "starting the promoted instance")
	require.NoError(t, s.db.StartInstance(ctx, StartInstanceOptions{
		InstanceID: s.promoted.ID,
		Force:      true,
	}))
	require.True(t, waitFor(func() bool {
		inRecovery, err := queryInstance[bool](ctx, s.db, s.promoted, "SELECT pg_is_in_recovery()")
		return err == nil && !inRecovery
	}, 2*time.Minute), "promoted instance should become the leader again")

	// The original instance only has this row if it followed the promoted
	// instance's timeline instead of starting its own.
	tLog(t, "confirming that the original instance follows the promoted instance")
	requireEventuallyHasRow(ctx, t, s.db, s.original, 2)
	inRecovery, err := queryInstance[bool](ctx, s.db, s.original, "SELECT pg_is_in_recovery()")
	require.NoError(t, err)
	assert.True(t, inRecovery, "original instance should be a replica")
}

func TestRestoreCommandPreventsTimelineReuse(t *testing.T) {
	t.Parallel()

	fixture.SkipIfS3Unsupported(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	s := newTimelineScenario(ctx, t, "restore_command", &controlplane.BackupConfigSpec{
		Repositories: []*controlplane.BackupRepositorySpec{
			fixture.S3BackupRepository(),
		},
	})

	// The original instance has never seen the promoted instance's history
	// file, so it can only learn about that timeline from the archive.
	historyFile := fmt.Sprintf("%08X.history", s.timeline)
	tLogf(t, "waiting for %s to be archived", historyFile)
	require.True(t, waitFor(func() bool {
		archived, err := queryInstance[bool](ctx, s.db, s.promoted,
			"SELECT EXISTS (SELECT 1 FROM pg_ls_archive_statusdir() WHERE name = $1)",
			historyFile+".done")
		return err == nil && archived
	}, 2*time.Minute), "%s should be archived", historyFile)

	s.startStaleOriginal(ctx, t)

	// A failover to a named candidate bypasses check_timeline.
	tLog(t, "forcing a failover to the original instance")
	require.NoError(t, s.db.FailoverDatabaseNode(ctx, &controlplane.FailoverDatabaseNodeRequest{
		DatabaseID:          controlplane.Identifier(s.db.ID),
		NodeName:            "n1",
		CandidateInstanceID: &s.original.ID,
	}))
	require.True(t, waitFor(func() bool {
		inRecovery, err := queryInstance[bool](ctx, s.db, s.original, "SELECT pg_is_in_recovery()")
		return err == nil && !inRecovery
	}, 2*time.Minute), "original instance should be promoted by the failover")

	timeline, err := queryInstance[int](ctx, s.db, s.original, currentTimelineQuery)
	require.NoError(t, err)
	assert.Equal(t, s.timeline+1, timeline,
		"original instance should skip the timeline in the archive instead of reusing it")
}

// currentTimelineQuery returns the current timeline of a primary instance from
// the name of its current WAL segment, which begins with the timeline as eight
// hex digits.
const currentTimelineQuery = "SELECT ('x' || substr(pg_walfile_name(pg_current_wal_lsn()), 1, 8))::bit(32)::int"

func queryInstance[T any](
	ctx context.Context,
	db *DatabaseFixture,
	instance *controlplane.Instance,
	sql string,
	args ...any,
) (T, error) {
	var result T

	conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
		Instance: instance,
		Username: "admin",
		Password: "password",
	})
	if err != nil {
		return result, err
	}
	defer conn.Close(ctx)

	err = conn.QueryRow(ctx, sql, args...).Scan(&result)

	return result, err
}

func execInstance(
	ctx context.Context,
	db *DatabaseFixture,
	instance *controlplane.Instance,
	sql string,
) error {
	conn, err := db.ConnectToInstance(ctx, ConnectionOptions{
		Instance: instance,
		Username: "admin",
		Password: "password",
	})
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, sql)

	return err
}

func requireEventuallyHasRow(
	ctx context.Context,
	t *testing.T,
	db *DatabaseFixture,
	instance *controlplane.Instance,
	id int,
) {
	t.Helper()

	require.True(t, waitFor(func() bool {
		exists, err := queryInstance[bool](ctx, db, instance,
			"SELECT EXISTS (SELECT 1 FROM timeline_test WHERE id = $1)", id)
		return err == nil && exists
	}, 2*time.Minute), "instance %s should have row %d", instance.ID, id)
}

func getHost(ctx context.Context, t *testing.T, hostID string) *controlplane.Host {
	t.Helper()

	host, err := fixture.Client.GetHost(ctx, &controlplane.GetHostPayload{
		HostID: controlplane.Identifier(hostID),
	})
	require.NoError(t, err)

	return host
}

// runPatronictl runs patronictl against the instance's Patroni cluster from
// the instance's host.
func runPatronictl(host *controlplane.Host, instance *controlplane.Instance, args string) (string, error) {
	switch host.Orchestrator {
	case "swarm":
		// The container's environment points patronictl at its config.
		containerName, err := swarmContainerName(instance)
		if err != nil {
			return "", err
		}
		return fixture.RunCmdOnHost(instance.HostID, fmt.Sprintf(
			"docker exec %s patronictl %s", containerName, args))
	case "systemd":
		// The config's location depends on the host's OS and configuration,
		// so it's read from the Patroni unit, whose ExecStart is
		// '<patroni> <config>'.
		return fixture.RunCmdOnHost(instance.HostID, fmt.Sprintf(
			`patronictl -c "$(systemctl cat patroni-%s.service | awk '/^ExecStart=/ {print $NF}')" %s`,
			instance.ID, args))
	default:
		return "", fmt.Errorf("unsupported orchestrator %q", host.Orchestrator)
	}
}

// patroniHistory returns the timelines in the history that Patroni has
// recorded for the instance's cluster.
func patroniHistory(host *controlplane.Host, instance *controlplane.Instance) ([]int, error) {
	out, err := runPatronictl(host, instance, "history --format json")
	if err != nil {
		return nil, err
	}
	// The output can be preceded by warnings.
	start := strings.Index(out, "[")
	if start < 0 {
		return nil, errors.New("patronictl history output has no JSON array")
	}
	var entries []struct {
		Timeline int `json:"TL"`
	}
	if err := json.Unmarshal([]byte(out[start:]), &entries); err != nil {
		return nil, fmt.Errorf("failed to parse patronictl history output: %w", err)
	}
	timelines := make([]int, len(entries))
	for i, e := range entries {
		timelines[i] = e.Timeline
	}

	return timelines, nil
}
