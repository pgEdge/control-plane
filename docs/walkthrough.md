---
cwd: ../
---

# Guided Walkthrough

This walkthrough guides you through deploying a distributed PostgreSQL
database using the pgEdge Control Plane, a lightweight orchestrator
that manages Postgres databases with multi-master replication and
read replica support. By the end you will have a running database
with three nodes, each accepting reads and writes.

| Step | What you'll do |
|------|---------------|
| Start the Control Plane | Launch the orchestrator in a Docker container |
| Create a Distributed Database | Deploy a 3-node Postgres database with Spock replication |
| Verify Multi-Master Replication | Write on one node, read from another |
| Test Resilience | Take a node down, verify recovery without data loss |

> [!TIP]
> **Run the commands as you read**
> Every code block below is executable. Open this repo in
> [GitHub Codespaces](https://codespaces.new/pgEdge/control-plane?devcontainer_path=.devcontainer/walkthrough/devcontainer.json)
> for a ready-to-go environment, or install the
> [Runme extension](https://marketplace.visualstudio.com/items?itemName=stateful.runme)
> in VS Code and click **Execute Cell** on each block.

## Prerequisites

- [Docker Engine](https://docs.docker.com/engine/install/) (Linux) or [Docker Desktop](https://docs.docker.com/desktop/) (macOS)
- [curl](https://curl.se/download.html)
- [jq](https://jqlang.github.io/jq/download/)
- psql
    - macOS: `brew install libpq && brew link --force libpq`
    - Linux: [pgEdge Enterprise packages](https://docs.pgedge.com/enterprise) or your distribution's `postgresql-client` package

> [!WARNING]
> **macOS: Enable host networking in Docker Desktop**
> The Control Plane requires Docker host networking. On macOS with Docker
> Desktop, this must be enabled manually. Open Docker Desktop and go
> to **Settings > Resources > Network**, check **Enable host
> networking**, then click **Apply and restart**. See
> [Docker Desktop host networking](https://docs.docker.com/engine/network/drivers/host/#docker-desktop)
> for details.

## Step 1: Start the Control Plane

The Control Plane is a lightweight orchestrator that manages your Postgres
instances. It runs on each of your hosts and exposes a REST API.
This example runs on a single host.

### If you're in Codespaces

The environment is already set up. Skip ahead to
[Set up the environment](#set-up-the-environment).

### On your own machine

Run the bootstrap script to download the walkthrough files and check
prerequisites. It will ask how you'd like to continue — choose the
interactive guide for a terminal experience, or exit to follow this
document at your own pace:

```text
curl -fsSL https://raw.githubusercontent.com/pgEdge/control-plane/main/examples/walkthrough/install.sh | bash
```

### Prefer a guided terminal experience?

The interactive guide walks you through the same steps with prompts
and spinners:

```text
bash examples/walkthrough/guide.sh
```

### Set up the environment

The Control Plane uses Docker Swarm for container orchestration.
Initialize Swarm and set the ports for each database node. Adjust the
ports if they conflict with existing services on your machine:

```bash
if [ "$(docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null)" = "active" ]; then
  echo "Swarm already active"
else
  docker swarm init
fi

export N1_PORT="5432"
export N2_PORT="5433"
export N3_PORT="5434"
export CP_DATA="/tmp/pgedge-cp-demo"
mkdir -p "$CP_DATA"
```

> [!TIP]
> **Getting a 'could not choose an IP address' error?**
> If your machine has multiple network interfaces, Docker needs you
> to specify which address to advertise. Find your primary IP and
> run `docker swarm init --advertise-addr <your-ip>` instead.

### Start the Control Plane

Pull the Control Plane image, start it, and initialize:

```bash
docker pull ghcr.io/pgedge/control-plane
docker run --detach \
    --env PGEDGE_HOST_ID=host-1 \
    --env PGEDGE_DATA_DIR="$CP_DATA" \
    --volume "$CP_DATA":"$CP_DATA" \
    --volume /var/run/docker.sock:/var/run/docker.sock \
    --network host \
    --name host-1 \
    ghcr.io/pgedge/control-plane \
    run
echo "Waiting for Control Plane API..."
until curl -sf http://localhost:3000/v1/version >/dev/null 2>&1; do
  sleep 2
done
echo "Control Plane is ready!"

status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/v1/cluster/init)
case "$status" in
  200|201) echo "Initialized." ;;
  409)     echo "Already initialized." ;;
  *)       echo "Initialization failed (HTTP $status)"; exit 1 ;;
esac
```

## Step 2: Create a Distributed Database

### What you're creating

The Control Plane uses a declarative model. You describe the database you
want and the Control Plane handles the configuration and deployment.

The database spec defines three nodes — n1, n2, and n3. Each node
runs its own Postgres primary and accepts reads and writes
independently. Spock logical replication keeps all nodes in sync
by replicating changes bidirectionally. Nodes can also have read
replicas for high availability, though this walkthrough focuses on
multi-master replication.

This will create a database with three nodes.

### Create the database

```bash
curl -s -X POST http://localhost:3000/v1/databases \
    -H "Content-Type: application/json" \
    --data '{
        "id": "example",
        "spec": {
            "database_name": "example",
            "database_users": [
                {
                    "username": "admin",
                    "password": "password",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "nodes": [
                { "name": "n1", "port": '"$N1_PORT"', "host_ids": ["host-1"] },
                { "name": "n2", "port": '"$N2_PORT"', "host_ids": ["host-1"] },
                { "name": "n3", "port": '"$N3_PORT"', "host_ids": ["host-1"] }
            ]
        }
    }' | jq .task
```

The Control Plane API returns a task confirming that database creation has started.
Creation is asynchronous — the database and its nodes are being set up
in the background.

### Wait for the database

Poll until the state is `available`. This may take a few minutes on
the first run:

```bash
echo "Waiting for database..."
while true; do
  STATE=$(curl -sf \
    http://localhost:3000/v1/databases/example | jq -r '.state')
  echo "  State: $STATE"
  [ "$STATE" = "available" ] && break
  sleep 3
done
echo "Database is ready!"
```

### Explore the database through the API

The Control Plane API provides full visibility into your database —
nodes, instances, state, and connection info:

```bash
curl -s http://localhost:3000/v1/databases/example | jq .
```

### Verify with psql

Connect to n1 to confirm Postgres is running:

```bash
PGPASSWORD=password psql -h localhost -p "$N1_PORT" -U admin example -c "SELECT version();"
```

## Step 3: Verify Multi-Master Replication

All three nodes have Spock bidirectional replication. Every node
accepts writes and changes propagate automatically.

### Create a table on n1

```bash
PGPASSWORD=password psql -h localhost -p "$N1_PORT" -U admin example \
    -c "CREATE TABLE example (id int primary key, data text);"
```

### Insert a row on n2

```bash
PGPASSWORD=password psql -h localhost -p "$N2_PORT" -U admin example \
    -c "INSERT INTO example (id, data) VALUES (1, 'Hello from n2!');"
```

### Read it back from n1

The row was written on n2 but is already on n1 via Spock replication:

```bash
PGPASSWORD=password psql -h localhost -p "$N1_PORT" -U admin example \
    -c "SELECT * FROM example;"
```

### Write on n3

```bash
PGPASSWORD=password psql -h localhost -p "$N3_PORT" -U admin example \
    -c "INSERT INTO example (id, data) VALUES (2, 'Hello from n3!');"
```

### Read from n1 again

Both rows should be here — one replicated from n2, one from n3:

```bash
PGPASSWORD=password psql -h localhost -p "$N1_PORT" -U admin example \
    -c "SELECT * FROM example;"
```

Both rows replicated to n1. Every node can read every other node's
writes.

## Step 4: Test Resilience

### What's happening

Active-active means every node accepts reads and writes. If a node
goes down, the others keep working. When it comes back, Spock
automatically catches it up.

You'll simulate a node failure by taking n2 offline, write data
while it's down, then bring it back and verify everything replicated.

### Take n2 offline

```bash
N2_SERVICE=$(docker service ls \
  --filter label=pgedge.component=postgres \
  --filter label=pgedge.node.name=n2 \
  --format '{{ .Name }}')
docker service scale "$N2_SERVICE"=0
echo "Node n2 scaled to 0."
```

Check how the Control Plane sees the database now:

```bash
curl -s http://localhost:3000/v1/databases/example | jq '.instances[] | {node_name, state}'
```

### Write on n1 while n2 is down

```bash
PGPASSWORD=password psql -h localhost -p "$N1_PORT" -U admin example \
    -c "INSERT INTO example (id, data) VALUES (3, 'Written while n2 is down!');"
```

### Read from n3 to confirm the database still works

```bash
PGPASSWORD=password psql -h localhost -p "$N3_PORT" -U admin example \
    -c "SELECT * FROM example;"
```

The database kept working with a node down.

### Bring n2 back online

```bash
N2_SERVICE=$(docker service ls \
  --filter label=pgedge.component=postgres \
  --filter label=pgedge.node.name=n2 \
  --format '{{ .Name }}')
docker service scale "$N2_SERVICE"=1
echo "Node n2 scaling back up."
```

### Wait for n2 to recover

Poll until n2 is accepting connections and replication has synced:

```bash
echo "Waiting for n2 to accept connections..."
until PGPASSWORD=password psql -h localhost -p "$N2_PORT" -U admin example \
    -c "SELECT 1" >/dev/null 2>&1; do
  sleep 3
done
echo "n2 is back. Waiting for replication sync..."
until PGPASSWORD=password psql -h localhost -p "$N2_PORT" -U admin example \
    -tAc "SELECT 1 FROM example WHERE id = 3;" | grep -qx '1'; do
  sleep 3
done
echo "Replication is synced."
```

### Check the database state

```bash
curl -s http://localhost:3000/v1/databases/example | jq '.instances[] | {node_name, state}'
```

All nodes should be back.

### Read from n2 to verify recovery

Everything should be here, including the row written while n2 was
down:

```bash
PGPASSWORD=password psql -h localhost -p "$N2_PORT" -U admin example \
    -c "SELECT * FROM example;"
```

The database survived a node failure. n2 came back online and Spock
replication caught everything up without data loss.

## Explore Further

| Command | What it does |
|---------|-------------|
| `curl -s http://localhost:3000/v1/databases/example \| jq` | Full database status (includes nodes) |
| `curl -s http://localhost:3000/v1/databases/example \| jq '.instances'` | List just the instances |
| `curl -s http://localhost:3000/v1/version \| jq` | Control Plane version |
| `docker service ls` | List all Swarm services |

## Cleanup

If you are running in GitHub Codespaces, delete the Codespace. No
other cleanup is needed.

If you are running locally:

```bash
# Remove database services (stops Postgres containers)
docker service rm $(docker service ls \
  --filter label=pgedge.database.id=example -q) 2>/dev/null || true

# Remove the Control Plane container
docker rm -f host-1 2>/dev/null || true

echo "Cleanup complete."
```

> [!NOTE]
> The temporary data directory created by `guide.sh` or the
> walkthrough blocks above requires `sudo rm -rf` to remove,
> since Docker creates files as root.

## Learn More

| Topic | Link |
|-------|------|
| Control Plane Overview | [docs.pgedge.com/control-plane](https://docs.pgedge.com/control-plane/) |
| Control Plane Concepts | [docs.pgedge.com/control-plane/prerequisites/concepts](https://docs.pgedge.com/control-plane/prerequisites/concepts) |
| Spock Multi-Master Replication | [docs.pgedge.com/spock-v5](https://docs.pgedge.com/spock-v5) |
| API Reference | [docs.pgedge.com/control-plane/api/reference](https://docs.pgedge.com/control-plane/api/reference) |
| Package Catalog | [docs.pgedge.com/enterprise/packages](https://docs.pgedge.com/enterprise/packages) |
