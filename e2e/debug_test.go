//go:build e2e_test

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

func debugWriteOutput(dir, filename, contents string) {
	path := filepath.Join(dir, filename)

	if len(contents) == 0 {
		log.Printf("skipping empty write for debug file '%s'\n", path)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("failed to make parent directory '%s': %s\n", dir, err)
		return
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		log.Printf("failed to write debug file '%s': %s\n", path, err)
	}
}

func debugWriteJSON(dir, filename string, data any) {
	if data == nil {
		return
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		path := filepath.Join(dir, filename)
		log.Printf("failed to marshal data for '%s': %s\n", path, err)
		return
	}
	debugWriteOutput(dir, filename, string(raw))
}

func debugRunCmd(hostID, cmd string) string {
	out, err := fixture.RunCmdOnHost(hostID, cmd)
	if err != nil {
		log.Printf("failed to run cmd %q\n", cmd)
		return ""
	}
	return out
}

func debugDockerCmd(hostID, cmd string, filters ...string) string {
	for _, filter := range filters {
		cmd += " --filter " + filter
	}
	return debugRunCmd(hostID, cmd)
}

func debugServiceList(filters ...string) string {
	host1 := fixture.HostIDs()[0]

	return debugDockerCmd(host1, "docker service ls", filters...)
}

func debugServiceNames(filters ...string) string {
	host1 := fixture.HostIDs()[0]

	return debugDockerCmd(host1, "docker service ls --format '{{ .Name }}'", filters...)
}

func debugServiceInspect(name string) string {
	host1 := fixture.HostIDs()[0]

	return debugDockerCmd(host1, "docker service inspect "+name)
}

func debugServiceLogs(name string) string {
	host1 := fixture.HostIDs()[0]

	return debugDockerCmd(host1, "docker service logs "+name)
}

func debugServicePs(name string) string {
	host1 := fixture.HostIDs()[0]

	return debugDockerCmd(host1, "docker service ps "+name)
}

func debugNetworkInspect(name string) string {
	host1 := fixture.HostIDs()[0]

	return debugDockerCmd(host1, "docker network inspect "+name)
}

func debugContainerList(hostID string, filters ...string) string {
	return debugDockerCmd(hostID, "docker ps -a", filters...)
}

func debugContainerLogs(hostID string, since time.Time, filters ...string) string {
	ids := debugDockerCmd(hostID, `docker ps -a --format '{{ .ID }}'`, filters...)

	var logs string
	for _, id := range slices.Backward(strings.Fields(ids)) {
		logsCmd := fmt.Sprintf(`docker logs %s`, id)
		if !since.IsZero() {
			logsCmd += ` --since ` + since.Format(time.RFC3339)
		}
		logs += debugRunCmd(hostID, logsCmd)
	}

	return logs
}

func debugWriteControlPlaneInfo(outputDir string, since time.Time) {
	hostIDs := fixture.HostIDs()
	for _, hostID := range hostIDs {
		filter := fmt.Sprintf(`'name=control-plane.*%s'`, hostID)

		ps := debugContainerList(hostID, filter)
		psFilename := fmt.Sprintf("%s-docker-ps.txt", hostID)
		debugWriteOutput(outputDir, psFilename, ps)

		logs := debugContainerLogs(hostID, since, filter)
		logsFilename := fmt.Sprintf("%s.log", hostID)
		debugWriteOutput(outputDir, logsFilename, logs)
	}

	hostList, err := fixture.Client.ListHosts(context.Background())
	if err != nil {
		log.Printf("failed to list control plane hosts: %s\n", err)
	} else {
		debugWriteJSON(outputDir, "list-hosts.json", hostList)
	}
}

func debugWriteTaskInfo(outputDir, databaseID string) {
	ctx := context.Background()
	tasks, err := fixture.Client.ListDatabaseTasks(ctx, &controlplane.ListDatabaseTasksPayload{
		DatabaseID: controlplane.Identifier(databaseID),
	})
	if err != nil {
		log.Printf("failed to list tasks for database '%s': %s\n", databaseID, err)
		return
	}
	debugWriteJSON(outputDir, "tasks.json", tasks)

	dir := filepath.Join(outputDir, "tasks")

	for _, task := range tasks.Tasks {
		taskLog, err := fixture.Client.GetDatabaseTaskLog(ctx, &controlplane.GetDatabaseTaskLogPayload{
			DatabaseID: controlplane.Identifier(databaseID),
			TaskID:     task.TaskID,
		})
		if err != nil {
			log.Printf("failed to get task '%s' log for database '%s': %s\n", task.TaskID, databaseID, err)
			continue
		}
		filename := fmt.Sprintf("%s-log.json", task.TaskID)
		debugWriteJSON(dir, filename, taskLog)
	}
}

func debugWriteInstanceInfo(outputDir string, instance *controlplane.Instance) {
	ctx := context.Background()
	dir := filepath.Join(outputDir, instance.ID)

	filters := []string{
		`label=pgedge.component=postgres`,
		`label=pgedge.instance.id=` + instance.ID,
	}

	// Sometimes service logs can be incomplete, so it can still be useful to
	// collect container logs.
	containerLogs := debugContainerLogs(instance.HostID, time.Time{}, filters...)
	debugWriteOutput(dir, "container.log", containerLogs)

	ps := debugContainerList(instance.HostID, filters...)
	debugWriteOutput(dir, "docker-ps.txt", ps)

	host, err := fixture.Client.GetHost(ctx, &controlplane.GetHostPayload{
		HostID: controlplane.Identifier(instance.HostID),
	})
	if err != nil {
		fmt.Printf("failed to get host '%s': %s\n", instance.HostID, err)
		return
	}

	logsDir := filepath.Join(host.DataDir, "instances", instance.ID, "data", "pgdata", "log")
	logFilenames := debugRunCmd(instance.HostID, "ls -tr "+logsDir)

	postgresLogs := make([]string, 0, len(logFilenames))
	for logFilename := range strings.FieldsSeq(logFilenames) {
		logPath := filepath.Join(logsDir, logFilename)
		postgresLogs = append(postgresLogs, debugRunCmd(instance.HostID, "cat "+logPath))
	}

	debugWriteOutput(dir, "postgres.log", strings.Join(postgresLogs, "\n"))
}

func debugWriteDatabaseInfo(t testing.TB, outputDir, databaseID string) {
	ctx := context.Background()
	dir := filepath.Join(outputDir, t.Name(), databaseID)

	debugWriteTaskInfo(dir, databaseID)

	serviceFilters := []string{
		`label=pgedge.component=postgres`,
		`label=pgedge.database.id=` + databaseID,
	}
	services := debugServiceList(serviceFilters...)
	debugWriteOutput(dir, "services.txt", services)

	// We do this outside of debugWriteInstanceInfo to handle cases where the
	// instance fails before InstanceResource.Create because it won't be present
	// in the get database output.
	serviceNames := debugServiceNames(serviceFilters...)
	for serviceName := range strings.FieldsSeq(serviceNames) {
		inspect := debugServiceInspect(serviceName)
		debugWriteOutput(dir, serviceName+"-inspect.json", inspect)

		logs := debugServiceLogs(serviceName)
		debugWriteOutput(dir, serviceName+".log", logs)

		ps := debugServicePs(serviceName)
		debugWriteOutput(dir, serviceName+"-ps.txt", ps)
	}

	network := debugNetworkInspect(databaseID + "-database")
	debugWriteOutput(dir, "network-inspect.json", network)

	db, err := fixture.Client.GetDatabase(ctx, &controlplane.GetDatabasePayload{
		DatabaseID: controlplane.Identifier(databaseID),
	})
	if err != nil {
		log.Printf("failed to get database '%s': %s\n", databaseID, err)
		return
	}

	debugWriteJSON(dir, "get-database.json", db)

	for _, instance := range db.Instances {
		debugWriteInstanceInfo(dir, instance)
	}
}
