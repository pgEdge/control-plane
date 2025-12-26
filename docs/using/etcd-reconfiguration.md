# Etcd Mode Reconfiguration

This guide describes how to reconfigure a Control Plane host's etcd mode after initialization.

!!! warning "Important Considerations"
    - **Container restart required** - The host must be restarted for etcd mode changes to take effect
    - **Environment variables take precedence** - For containerized deployments, update `PGEDGE_ETCD_MODE` environment variable
    - **One host at a time** - Only reconfigure one host at a time to maintain cluster stability
    - **No data loss** - This process preserves existing data while changing etcd mode

## Overview

The Control Plane supports running hosts in two etcd modes:

- **Server mode**: The host runs an embedded etcd server and participates in the etcd cluster as a voting member
- **Client mode**: The host connects to the etcd cluster as a client only, without running its own etcd server

By default, the first 3 hosts in a cluster are configured as etcd servers during initial deployment. However, for optimal etcd cluster health:

- **Clusters with 1-3 hosts**: All hosts should run as etcd servers
- **Clusters with 4-7 hosts**: 3 hosts should run as etcd servers, others as clients
- **Clusters with 8+ hosts**: 5 hosts should run as etcd servers, others as clients

!!! warning "Odd Numbers Required"
    Etcd requires an **odd number** of voting members (servers) for proper quorum. Always maintain 3 or 5 etcd servers, not 4.

## When to Reconfigure

You should reconfigure a host's etcd mode in these scenarios:

### Initial Cluster Setup with Many Nodes
If you deployed a cluster with more than 3 nodes and all were initialized as servers, you should demote some hosts to client mode to maintain optimal etcd cluster size.

### Disaster Recovery
If an etcd server node fails permanently and needs to be replaced, promote a client to server mode to maintain proper quorum.

### Cluster Scaling
When adding or removing hosts from the cluster, you may need to adjust which hosts run etcd servers to maintain 3-5 servers total.

### Performance Optimization  
If a host is experiencing resource constraints, demoting it from server to client mode reduces overhead since it won't participate in etcd consensus.

## How It Works

The etcd mode reconfiguration process works by:

1. **Stopping the container** - This gracefully shuts down the current etcd process
2. **Updating configuration** - Change the `PGEDGE_ETCD_MODE` environment variable
3. **Restarting the container** - The host starts with the new etcd mode
4. **Automatic etcd membership** - When starting as a server, the host automatically adds itself to the etcd cluster

The key insight is that etcd mode can be changed by simply restarting with different environment variables.

## Prerequisites

- The cluster must be healthy and have at least 2 other healthy etcd servers
- For containerized deployments, you must be able to update environment variables
- Access to container management tools (docker, docker compose, etc.)

## Procedure: Promoting a Client to Server

Follow these steps to convert an etcd client host to server mode.

### Step 1: Verify Prerequisites and Plan

Ensure the cluster is healthy:

```bash
# Check current etcd servers
curl -s http://localhost:3000/v1/hosts | jq -r '.hosts[] | select(.status.components.etcd.healthy == true) | .id'

# Verify host-4 has NO etcd directory (it's a client)
ls -la ./docker/control-plane-dev/data/host-4/ | grep etcd
# Should show no etcd directory
```

### Step 2: Stop the Host

Stop the container to prepare for environment variable changes:

```bash
# Stop the Control Plane container
docker stop $(docker ps -q -f name=control-plane-dev-host-4-1)

```

### Step 3: Update Environment Variables and Restart

Update the `PGEDGE_ETCD_MODE` environment variable and restart the container.

**For Docker Compose**, edit `docker-compose.yaml`:

```yaml
host-4:
  environment:
    - PGEDGE_ETCD_MODE=server                    # Changed from 'client'
    - PGEDGE_ETCD_SERVER__PEER_PORT=2680         # Add peer port
    - PGEDGE_ETCD_SERVER__CLIENT_PORT=2679       # Add client port
```

Then restart the container:

```bash
# For Docker Compose, set WORKSPACE_DIR environment variable
WORKSPACE_DIR=/path/to/control-plane docker compose -f ./docker/control-plane-dev/docker-compose.yaml up -d host-4
# WORKSPACE_DIR=//Users/sivat/projects/control-plane/control-plane/control-plane docker compose -f ./docker/control-plane-dev/docker-compose.yaml up -d host-4
```

### Step 4: Rejoin the Cluster

After changing the etcd mode and restarting, the host must rejoin the cluster:

```bash
JOIN_TOKEN=$(restish control-plane-local-2 get-join-token)
restish control-plane-local-4 join-cluster "$JOIN_TOKEN"
```

Wait a few seconds for the host to fully rejoin and become healthy.

```bash
docker ps --format "table {{.Names}}\t{{.Status}}" | grep host-4
```

### Step 5: Remove Stale Etcd Member (If Exists)

If the host was previously running as an etcd server, check for and remove any stale member entries:

```bash

ETCD_USER=$(cat ./docker/control-plane-dev/data/host-1/generated.config.json | jq -r '.etcd_username')
ETCD_PASS=$(cat ./docker/control-plane-dev/data/host-1/generated.config.json | jq -r '.etcd_password')

# List current etcd members
docker exec $(docker ps -q -f name=control-plane-host-1) \
  etcdctl --endpoints=https://localhost:2379 \
  --cacert=/data/control-plane/certs/ca.crt \
  --cert=/data/control-plane/certs/etcd-user.crt \
  --key=/data/control-plane/certs/etcd-user.key \
  --user="$ETCD_USER:$ETCD_PASS" \
  member list -w table

# If you see an unstarted member for host-4, remove it:
# docker exec $(docker ps -q -f name=control-plane-host-1) \
#   etcdctl --endpoints=https://localhost:2379 \
#   --cacert=/data/control-plane/certs/ca.crt \
#   --cert=/data/control-plane/certs/etcd-user.crt \
#   --key=/data/control-plane/certs/etcd-user.key \
#   --user="$ETCD_USER:$ETCD_PASS" \
#   member remove <member-id-from-table>
```

### Step 6: Verify the Promotion

Confirm the host is now running as an etcd server and is reachable:

```bash
ROOT="/path/to/control-plane"
CERT_DIR="$ROOT/docker/control-plane-dev/data/host-1/certificates"
CONFIG="$ROOT/docker/control-plane-dev/data/host-1/generated.config.json"

user=$(jq -r '.etcd_username' "$CONFIG")
pass=$(jq -r '.etcd_password' "$CONFIG")

unset ETCDCTL_USER  # avoid env vs flag conflict

ETCDCTL_API=3 etcdctl \
  --endpoints="https://127.0.0.1:2379" \
  --cacert="$CERT_DIR/ca.crt" \
  --cert="$CERT_DIR/etcd-user.crt" \
  --key="$CERT_DIR/etcd-user.key" \
  --user="${user}:${pass}" \
  member list -w table

# Expected output should show host-4 with STATUS=started and IS_LEARNER=false

{"level":"warn","ts":"2025-12-02T23:57:12.594193+0530","caller":"flags/flag.go:94","msg":"unrecognized environment variable","environment-variable":"ETCDCTL_API=3"}
+------------------+---------+--------+---------------------------+---------------------------+------------+
|        ID        | STATUS  |  NAME  |        PEER ADDRS         |       CLIENT ADDRS        | IS LEARNER |
+------------------+---------+--------+---------------------------+---------------------------+------------+
| 138f26b5c2b6dbdb | started | host-2 | https://192.168.64.2:2480 | https://192.168.64.2:2479 |      false |
| 3dd67e51bd9b2501 | started | host-3 | https://192.168.64.2:2580 | https://192.168.64.2:2579 |      false |
| b71f75320dc06a6c | started | host-1 | https://192.168.64.2:2380 | https://192.168.64.2:2379 |      false |
| e23ebb77165f8c54 | started | host-4 | https://192.168.64.2:2680 | https://192.168.64.2:2679 |      false |
+------------------+---------+--------+---------------------------+---------------------------+------------+
# Verify etcd data directory was created on host-4
ls -la ./docker/control-plane-dev/data/host-4/etcd
# Should now exist with subdirectories

# Check cluster health
ETCDCTL_API=3 etcdctl \
  --endpoints="https://127.0.0.1:2379" \
  --cacert="$CERT_DIR/ca.crt" \
  --cert="$CERT_DIR/etcd-user.crt" \
  --key="$CERT_DIR/etcd-user.key" \
  --user="${user}:${pass}" \
  endpoint health --cluster


{"level":"warn","ts":"2025-12-03T00:09:26.224263+0530","caller":"flags/flag.go:94","msg":"unrecognized environment variable","environment-variable":"ETCDCTL_API=3"}
https://192.168.64.2:2679 is healthy: successfully committed proposal: took = 995.583µs
https://192.168.64.2:2379 is healthy: successfully committed proposal: took = 1.134208ms
https://192.168.64.2:2579 is healthy: successfully committed proposal: took = 1.117042ms
https://192.168.64.2:2479 is healthy: successfully committed proposal: took = 544.625µs

```

All endpoints should show as healthy.

## Procedure: Demoting a Server to Client

Follow these steps to convert an etcd server host to client mode.


### Step 1: Verify Safe to Demote

```bash
# List current etcd servers
curl -s http://localhost:3000/v1/hosts | jq -r '.hosts[] | select(.status.components.etcd.healthy == true) | .id' | wc -l

# Should show at least 4 servers before demoting one
```

### Step 2: Remove from Etcd Cluster First

Remove the host from etcd membership BEFORE stopping it:

```bash
ROOT="/path/to/control-plane"
CERT_DIR="$ROOT/docker/control-plane-dev/data/host-1/certificates"
CONFIG="$ROOT/docker/control-plane-dev/data/host-1/generated.config.json"

user=$(jq -r '.etcd_username' "$CONFIG")
pass=$(jq -r '.etcd_password' "$CONFIG")

unset ETCDCTL_USER

ETCDCTL_API=3 etcdctl \
  --endpoints="https://127.0.0.1:2379" \
  --cacert="$CERT_DIR/ca.crt" \
  --cert="$CERT_DIR/etcd-user.crt" \
  --key="$CERT_DIR/etcd-user.key" \
  --user="${user}:${pass}" \
  member list -w table

# Remove the member by ID
ETCDCTL_API=3 etcdctl \
  --endpoints="https://127.0.0.1:2379" \
  --cacert="$CERT_DIR/ca.crt" \
  --cert="$CERT_DIR/etcd-user.crt" \
  --key="$CERT_DIR/etcd-user.key" \
  --user="${user}:${pass}" \
  member remove <member-id>

  

```

### Step 3: Stop the Host

```bash
# Stop the host
docker stop $(docker ps -q -f name=control-plane-dev-host-4-1)
```

### Step 4: Update Environment Variables and Restart

**For Docker Compose**, edit `docker-compose.yaml`:

```yaml
host-4:
  environment:
    - PGEDGE_ETCD_MODE=client  # Changed from 'server'
    # Remove PGEDGE_ETCD_SERVER__PEER_PORT and CLIENT_PORT
  
```

```bash
# Remove etcd folder 
rm -rf /path/to/control-plane/docker/control-plane-dev/data/host-4/etcd

```

Then restart:

```bash
WORKSPACE_DIR=/path/to/control-plane docker compose -f ./docker/control-plane-dev/docker-compose.yaml up -d host-4
```

The host will restart in client mode and automatically connect to the existing etcd cluster.

### Step 5: Verify Demotion

```bash
# Verify host-4 is NOT in etcd member list
 ETCDCTL_API=3 etcdctl \                                                                                           
  --endpoints="https://127.0.0.1:2379" \
  --cacert="$CERT_DIR/ca.crt" \
  --cert="$CERT_DIR/etcd-user.crt" \
  --key="$CERT_DIR/etcd-user.key" \
  --user="${user}:${pass}" \
  member list -w table

```

## Troubleshooting


### Etcd Directory Not Created After Promotion

Check the container logs:

```bash
docker logs $(docker ps -q -f name=control-plane-dev-host-4-1) | grep -i etcd
```

Verify the environment variable was set correctly:

```bash
docker inspect $(docker ps -q -f name=control-plane-dev-host-4-1) | grep PGEDGE_ETCD_MODE
```

### Cluster Health Issues After Changes

Check etcd status:

```
ETCDCTL_API=3 etcdctl \
  --endpoints="https://127.0.0.1:2379" \
  --cacert="$CERT_DIR/ca.crt" \
  --cert="$CERT_DIR/etcd-user.crt" \
  --key="$CERT_DIR/etcd-user.key" \
  --user="${user}:${pass}" \
  endpoint status --cluster -w table
```

Look for any members with high latency or errors.

## Best Practices

1. **Plan topology during deployment** - Decide which hosts will be servers vs clients before initializing
2. **Maintain odd numbers** - Always keep 3 or 5 etcd servers, never 2, 4, or 6
3. **One change at a time** - Never reconfigure multiple hosts simultaneously
4. **Monitor during changes** - Watch etcd health metrics during and after reconfiguration
5. **Test in staging first** - Practice these procedures in a non-production environment
6. **Document your topology** - Keep records of which hosts are servers vs clients
7. **Backup before changes** - Always backup data before making topology changes

## Summary

Etcd mode reconfiguration is supported through a stop-and-restart workflow:

1. ✅ **Stop** the host container
2. ✅ **Update environment variables** to set the new etcd mode
3. ✅ **Remove stale members** from etcd if they exist
4. ✅ **Restart the container** with the new configuration
5. ✅ **Verify** the new etcd mode is active

The key is that etcd mode can be changed by simply restarting with different environment variables. The Control Plane automatically handles etcd cluster membership when a server-mode host starts.
