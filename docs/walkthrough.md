---
cwd: ../
---

# Guided Walkthrough

Deploy a 3-node distributed PostgreSQL database with active-active
multi-master replication using the Spock extension, all orchestrated
by pgEdge Control Plane.

| Step | What you'll do |
|------|---------------|
| **Start Control Plane** | Launch the orchestrator in a Docker container |
| **Create a Distributed Database** | Deploy a 3-node Postgres cluster with Spock replication |
| **Verify Multi-Master Replication** | Write on one node, read from another |
| **Resilience Demo** | Take a node down, prove zero data loss on recovery |

!!! tip "Run the commands as you read"
    Every code block below is executable. Open this repo in
    [GitHub Codespaces](https://codespaces.new/pgEdge/control-plane?devcontainer_path=.devcontainer/walkthrough/devcontainer.json)
    for a ready-to-go environment, or install the
    [Runme extension](https://marketplace.visualstudio.com/items?itemName=stateful.runme)
    in VS Code and click **Execute Cell** on each block.

---

## Prerequisites

- **Docker** — [Docker Engine](https://docs.docker.com/engine/install/)
  (Linux) or [Docker Desktop](https://docs.docker.com/desktop/) (macOS)
- **curl** — [curl.se/download](https://curl.se/download.html)
- **jq** — [jqlang.github.io/jq/download](https://jqlang.github.io/jq/download/)

!!! warning "macOS: Enable host networking in Docker Desktop"
    Control Plane requires Docker host networking. On macOS with Docker
    Desktop, this must be enabled manually. Open Docker Desktop and go
    to **Settings > Resources > Network**, check **Enable host
    networking**, then click **Apply and restart**. See
    [Docker Desktop host networking](https://docs.docker.com/engine/network/drivers/host/#docker-desktop)
    for details.

---

## Step 1: Start the Control Plane

Control Plane is a lightweight orchestrator that manages your Postgres
instances. It runs as a single container and exposes a REST API.

### If you're in Codespaces

The environment is already set up. Skip ahead to
[Initialize Docker Swarm](#initialize-docker-swarm).

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

### Initialize Docker Swarm

Control Plane uses Docker Swarm for container orchestration:

```bash
docker swarm init 2>/dev/null || echo "Swarm already active"
```

!!! tip "Getting a 'could not choose an IP address' error?"
    If your machine has multiple network interfaces, Docker needs you
    to specify which address to advertise. Find your primary IP and
    run `docker swarm init --advertise-addr <your-ip>` instead.

### Create a temporary data directory

Control Plane persists configuration and database state to a host
directory that gets mounted into the container:

```bash
export CP_DATA=$(mktemp -d)/pgedge-cp-demo
mkdir -p "$CP_DATA"
echo "Data directory: $CP_DATA"
```

### Pull and start the Control Plane container

This pulls the Control Plane image from the GitHub container registry
and starts it with host networking. The Docker socket is mounted so
that Control Plane can create and manage Postgres containers on your
behalf.

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
```

### Initialize the cluster

Cluster initialization tells Control Plane to set up its internal
state — registering this host, initializing the metadata store, and
preparing to accept database definitions. This is a one-time
operation:

```bash
curl -sf http://localhost:3000/v1/cluster/init
echo "Cluster initialized."
```

---

## Step 2: Create a Distributed Database

### What you're creating

Control Plane uses a declarative model. You describe the database you
want — name, users, and nodes — and Control Plane handles the rest.
Spock multi-master replication is configured automatically between all
nodes.

This will create a 3-node database with an admin user. It takes a
minute or two while Control Plane pulls the Postgres image and starts
each node.

!!! note
    Open a second terminal and run `watch docker ps` (or use the
    Containers view in Docker Desktop) — you'll want
    this for the rest of the demo.

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
                { "name": "n1", "port": 5432, "host_ids": ["host-1"] },
                { "name": "n2", "port": 5433, "host_ids": ["host-1"] },
                { "name": "n3", "port": 5434, "host_ids": ["host-1"] }
            ]
        }
    }'
```

The API returns a JSON task confirming that database creation has
started. Creation is asynchronous — Control Plane is now pulling the
Postgres image and spinning up three containers in the background.

### Wait for the database

Poll until the state is `available`:

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

### Verify with psql

Connect to n1 to confirm Postgres is running:

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n1 \
    --format '{{.Names}}') psql -U admin example -c "SELECT version();"
```

!!! tip "Have psql installed locally?"
    You can also connect directly from your host:
    ```text
    PGPASSWORD=password psql -h localhost -p 5432 -U admin example
    ```

---

## Step 3: Verify Multi-Master Replication

All three nodes have Spock bi-directional replication. Every node
accepts writes and changes propagate automatically.

### Create a table on n1

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n1 \
    --format '{{.Names}}') psql -U admin example \
    -c "CREATE TABLE example (id int primary key, data text);"
```

### Insert a row on n2

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n2 \
    --format '{{.Names}}') psql -U admin example \
    -c "INSERT INTO example (id, data) VALUES (1, 'Hello from n2!');"
```

### Read it back from n1

The row was written on n2 but is already on n1 via Spock replication:

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n1 \
    --format '{{.Names}}') psql -U admin example \
    -c "SELECT * FROM example;"
```

### Write on n3

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n3 \
    --format '{{.Names}}') psql -U admin example \
    -c "INSERT INTO example (id, data) VALUES (2, 'Hello from n3!');"
```

### Read from n1 again

Both rows should be here — one replicated from n2, one from n3:

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n1 \
    --format '{{.Names}}') psql -U admin example \
    -c "SELECT * FROM example;"
```

Both rows replicated to n1. Every node can read every other node's
writes.

---

## Step 4: Resilience Demo

### What's happening

Active-active means every node accepts reads and writes. If a node
goes down, the others keep working — and when it comes back, Spock
automatically catches it up.

You'll halt n2 using Docker service scaling, write data while it's
down, then bring it back and verify everything replicated.

Scaling the service to 0 cleanly stops n2 and prevents Control Plane
from auto-recovering it, so you can observe each step.

### Scale n2 to 0

```bash
N2_SERVICE=$(docker service ls \
  --filter label=pgedge.component=postgres \
  --filter label=pgedge.node.name=n2 \
  --format '{{ .Name }}')
docker service scale "$N2_SERVICE"=0
echo "Node n2 scaled to 0."
```

### Write on n1 while n2 is down

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n1 \
    --format '{{.Names}}') psql -U admin example \
    -c "INSERT INTO example (id, data) VALUES (3, 'Written while n2 is down!');"
```

### Read from n3 to confirm the cluster still works

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n3 \
    --format '{{.Names}}') psql -U admin example \
    -c "SELECT * FROM example;"
```

The cluster kept working with a node down.

### Scale n2 back to 1

```bash
N2_SERVICE=$(docker service ls \
  --filter label=pgedge.component=postgres \
  --filter label=pgedge.node.name=n2 \
  --format '{{ .Name }}')
docker service scale "$N2_SERVICE"=1
echo "Node n2 scaling back up."
```

### Wait for n2 to come back

Poll until the n2 container appears and is ready:

```bash
echo "Waiting for n2 container..."
until docker ps --filter label=pgedge.node.name=n2 \
    --format '{{.Names}}' | grep -q .; do
  sleep 3
done
echo "n2 is back! Waiting for Postgres to accept connections..."
until docker exec $(docker ps --filter label=pgedge.node.name=n2 \
    --format '{{.Names}}') psql -U admin example \
    -c "SELECT 1;" >/dev/null 2>&1; do
  sleep 2
done
echo "Ready — replication should be synced."
```

### Read from n2 to verify recovery

Everything should be here, including the row written while n2 was
down:

```bash
docker exec $(docker ps --filter label=pgedge.node.name=n2 \
    --format '{{.Names}}') psql -U admin example \
    -c "SELECT * FROM example;"
```

The cluster survived a node failure, n2 came back via service
scaling, and Spock replication caught everything up. Zero data loss.

---

## Explore Further

| Command | What it does |
|---------|-------------|
| `curl -s http://localhost:3000/v1/databases/example \| jq` | Full database status (includes nodes) |
| `curl -s http://localhost:3000/v1/databases/example \| jq '.nodes'` | List just the nodes |
| `curl -s http://localhost:3000/v1/version \| jq` | Control Plane version |
| `docker service ls` | List all Swarm services |

---

## Cleanup

If you are running in GitHub Codespaces, just delete the Codespace —
no cleanup needed.

If you are running locally:

```bash
# Remove database services (stops Postgres containers)
docker service rm $(docker service ls \
  --filter label=pgedge.database.id=example -q) 2>/dev/null || true

# Remove the Control Plane container
docker rm -f host-1 2>/dev/null || true

echo "Cleanup complete."
```

!!! note
    The temporary data directory created by `guide.sh` or the
    walkthrough blocks above requires `sudo rm -rf` to remove,
    since Docker creates files as root.

---

## Learn More

| Topic | Link |
|-------|------|
| Control Plane docs | [docs.pgedge.com/control-plane](https://docs.pgedge.com/control-plane/) |
| Core concepts | [docs.pgedge.com/control-plane/prerequisites/concepts](https://docs.pgedge.com/control-plane/prerequisites/concepts) |
| Spock multi-master | [docs.pgedge.com/spock-v5](https://docs.pgedge.com/spock-v5) |
| API Reference | [docs.pgedge.com/control-plane/api/reference](https://docs.pgedge.com/control-plane/api/reference) |
| Package Catalog | [docs.pgedge.com/enterprise/packages](https://docs.pgedge.com/enterprise/packages) |
