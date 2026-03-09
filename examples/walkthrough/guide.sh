#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=runner.sh
source "$SCRIPT_DIR/runner.sh"

CP_IMAGE="${CP_IMAGE:-ghcr.io/pgedge/control-plane}"
CP_CONTAINER="host-1"
CP_URL="http://localhost:3000"
CP_DATA="$(mktemp -d)/pgedge-cp-demo"
DB_ID="example"
OS="$(uname -s)"

cleanup() {
  stop_spinner
}
trap cleanup EXIT

# ── Prerequisites ────────────────────────────────────────────────────────────

# shellcheck source=setup.sh
bash "$SCRIPT_DIR/setup.sh"

# ── Port detection ───────────────────────────────────────────────────────────

port_in_use() {
  if [[ "$OS" == "Darwin" ]]; then
    lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  else
    ss -tln 2>/dev/null | grep -q ":${1} "
  fi
}

detect_ports() {
  local preferred=(5432 5433 5434)
  local all_free=true

  for p in "${preferred[@]}"; do
    if port_in_use "$p"; then
      all_free=false
      break
    fi
  done

  if [[ "$all_free" == "true" ]]; then
    N1_PORT=5432
    N2_PORT=5433
    N3_PORT=5434
    return
  fi

  # Find 3 consecutive free ports starting from 5432
  local start=5432
  while true; do
    local p1="$start"
    local p2=$((start + 1))
    local p3=$((start + 2))
    if ! port_in_use "$p1" && ! port_in_use "$p2" && ! port_in_use "$p3"; then
      N1_PORT="$p1"
      N2_PORT="$p2"
      N3_PORT="$p3"
      break
    fi
    start=$((start + 1))
    if [[ "$start" -gt 65533 ]]; then
      error "Could not find 3 consecutive free ports."
      exit 1
    fi
  done

  warn "Standard Postgres ports (5432-5434) are in use."
  explain "Using available ports instead: ${BOLD}${N1_PORT}, ${N2_PORT}, ${N3_PORT}${RESET}"
  echo ""
}

# ── Welcome ──────────────────────────────────────────────────────────────────

header "pgEdge Enterprise Postgres"

explain "This guide walks you through deploying pgEdge Enterprise Postgres"
explain "using the Control Plane, a lightweight orchestrator that manages"
explain "distributed Postgres databases with multi-master replication and"
explain "read replica support."
explain ""
explain "  1. Start the Control Plane"
explain "  2. Create a distributed database"
explain "  3. Verify multi-master replication"
explain "  4. Demonstrate automatic recovery from node failure"
explain ""
explain "You'll go from zero to active-active replication in minutes."

prompt_continue

# ── Step 1: Start Control Plane ──────────────────────────────────────────────

header "Step 1: Start the Control Plane"

explain "The Control Plane is a lightweight orchestrator that manages your Postgres"
explain "instances. It runs on each of your hosts and exposes a REST API."
explain "In this example, we are running it on a single host."

# Remove stale container from a previous run
if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^${CP_CONTAINER}$"; then
  if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CP_CONTAINER}$"; then
    info "Removing stale container from a previous run..."
    docker rm -f "${CP_CONTAINER}" >/dev/null 2>&1 || true
  fi
fi

# Check if already running
if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CP_CONTAINER}$"; then
  info "Control Plane is already running (container: ${CP_CONTAINER})"
else
  # Initialize Docker Swarm if needed
  if [[ "$(docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null)" != "active" ]]; then
    info "Initializing Docker Swarm..."
    if [[ "$OS" == "Darwin" ]]; then
      if ! docker swarm init >/dev/null 2>&1; then
        error "Failed to initialize Docker Swarm. Try manually: docker swarm init"
        exit 1
      fi
    else
      local_addr=$(ip -4 route get 1.1.1.1 2>/dev/null | grep -oP 'src \K\S+' || true)
      if [[ -n "$local_addr" ]]; then
        if ! docker swarm init --advertise-addr "$local_addr" >/dev/null 2>&1; then
          error "Failed to initialize Docker Swarm. Try manually: docker swarm init --advertise-addr $local_addr"
          exit 1
        fi
      else
        if ! docker swarm init >/dev/null 2>&1; then
          error "Failed to initialize Docker Swarm. Try manually: docker swarm init"
          exit 1
        fi
      fi
    fi
  fi

  # Pull and start Control Plane
  mkdir -p "$CP_DATA"

  start_spinner "Pulling Control Plane image..."
  docker pull "$CP_IMAGE" >/dev/null 2>&1
  stop_spinner
  info "Image pulled: $CP_IMAGE"

  start_spinner "Starting Control Plane..."
  docker run --detach \
    --env PGEDGE_HOST_ID="${CP_CONTAINER}" \
    --env PGEDGE_DATA_DIR="${CP_DATA}" \
    --volume "${CP_DATA}":"${CP_DATA}" \
    --volume /var/run/docker.sock:/var/run/docker.sock \
    --network host \
    --name "${CP_CONTAINER}" \
    "$CP_IMAGE" \
    run >/dev/null 2>&1
  stop_spinner
  info "Container started: $CP_CONTAINER"

  # Wait for API
  start_spinner "Waiting for Control Plane API..."
  retries=30
  while [[ "$retries" -gt 0 ]]; do
    if curl -sf "http://localhost:3000/v1/version" >/dev/null 2>&1; then
      break
    fi
    sleep 2
    retries=$((retries - 1))
  done
  stop_spinner

  if [[ "$retries" -eq 0 ]]; then
    error "Control Plane did not become healthy within 60 seconds."
    exit 1
  fi
  info "Control Plane running on ${CP_URL}"
fi

# Initialize cluster (idempotent -- safe to re-run)
init_status=$(curl -s -o /dev/null -w "%{http_code}" "${CP_URL}/v1/cluster/init" 2>/dev/null || true)
case "$init_status" in
  200|201) info "Cluster initialized." ;;
  409)     info "Cluster already initialized." ;;
  *)
    error "Cluster initialization failed (HTTP ${init_status:-no response})."
    error "Check Control Plane logs: docker logs ${CP_CONTAINER}"
    exit 1
    ;;
esac

# Detect ports -- if the database already exists, read its ports from the API
# so that reruns target the correct instances.
existing_db=$(curl -sf "${CP_URL}/v1/databases/${DB_ID}" 2>/dev/null || true)
if [[ -n "$existing_db" ]]; then
  N1_PORT=$(echo "$existing_db" | jq -r '[.instances[] | select(.node_name=="n1")] | .[0].connection_info.port // empty')
  N2_PORT=$(echo "$existing_db" | jq -r '[.instances[] | select(.node_name=="n2")] | .[0].connection_info.port // empty')
  N3_PORT=$(echo "$existing_db" | jq -r '[.instances[] | select(.node_name=="n3")] | .[0].connection_info.port // empty')
  if [[ -n "$N1_PORT" && -n "$N2_PORT" && -n "$N3_PORT" ]]; then
    info "Existing database found. Using ports: n1=${N1_PORT}, n2=${N2_PORT}, n3=${N3_PORT}"
  else
    detect_ports
  fi
else
  detect_ports
fi

prompt_continue

# ── Step 2: Create a Distributed Database ────────────────────────────────────

header "Step 2: Create a Distributed Database"

explain "Control Plane uses a declarative model. You describe the database you"
explain "want and Control Plane handles the configuration and deployment for you."
explain ""
explain "A node represents an independent Postgres instance within your database."
explain "Each node accepts reads and writes, and Spock logical replication keeps"
explain "them in sync. Control Plane also supports read replicas for scaling read"
explain "traffic, though this walkthrough focuses on multi-master replication."
explain ""
explain "This will create a database with 3 nodes. It takes a minute or two as"
explain "the Postgres image is pulled and started on each node."

prompt_run "curl -s -X POST ${CP_URL}/v1/databases \\
    -H 'Content-Type: application/json' \\
    --data '{
        \"id\": \"${DB_ID}\",
        \"spec\": {
            \"database_name\": \"${DB_ID}\",
            \"database_users\": [
                {
                    \"username\": \"admin\",
                    \"password\": \"password\",
                    \"db_owner\": true,
                    \"attributes\": [\"SUPERUSER\", \"LOGIN\"]
                }
            ],
            \"nodes\": [
                { \"name\": \"n1\", \"port\": ${N1_PORT}, \"host_ids\": [\"host-1\"] },
                { \"name\": \"n2\", \"port\": ${N2_PORT}, \"host_ids\": [\"host-1\"] },
                { \"name\": \"n3\", \"port\": ${N3_PORT}, \"host_ids\": [\"host-1\"] }
            ]
        }
    }'" "Creating database..."

explain "Control Plane is now creating services for each node and starting"
explain "the Postgres containers."
echo ""

start_spinner "Waiting for database to become available..."
state=""
retries=60
while [[ "$retries" -gt 0 ]]; do
  state=$(curl -sf "${CP_URL}/v1/databases/${DB_ID}" 2>/dev/null | grep -o '"state":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
  if [[ "$state" == "available" ]]; then
    break
  fi
  sleep 3
  retries=$((retries - 1))
done
stop_spinner

if [[ "$state" == "available" ]]; then
  info "Database '${DB_ID}' is available with 3 nodes (n1, n2, n3)"
else
  warn "Database is still being created (state: ${state:-unknown}). You can check progress with:"
  show_cmd "curl -s ${CP_URL}/v1/databases/${DB_ID} | jq .state"
  prompt_continue
fi

explain ""
explain "Let's connect to n1 to confirm Postgres is running:"

prompt_run "PGPASSWORD=password psql -h localhost -p ${N1_PORT} -U admin ${DB_ID} -c \"SELECT version();\""

prompt_continue

# ── Step 3: Verify Multi-Master Replication ──────────────────────────────────

header "Step 3: Verify Multi-Master Replication"

explain "All three nodes have Spock bi-directional replication. Every node"
explain "accepts writes and changes propagate automatically."
explain ""
explain "Let's prove it. First, create a table on n1:"

# Clean up any leftover data from a previous run
PGPASSWORD=password psql -h localhost -p "${N1_PORT}" -U admin "${DB_ID}" \
  -c "DROP TABLE IF EXISTS example;" >/dev/null 2>&1 || true

prompt_run "PGPASSWORD=password psql -h localhost -p ${N1_PORT} -U admin ${DB_ID} -c \"CREATE TABLE example (id int primary key, data text);\""

explain "Insert a row on n2:"

prompt_run "PGPASSWORD=password psql -h localhost -p ${N2_PORT} -U admin ${DB_ID} -c \"INSERT INTO example (id, data) VALUES (1, 'Hello from n2!');\""

explain "Read it back from n1 -- it should be there via Spock replication:"

prompt_run "PGPASSWORD=password psql -h localhost -p ${N1_PORT} -U admin ${DB_ID} -c \"SELECT * FROM example;\""

explain "Now write on n3 and read from n1:"

prompt_run "PGPASSWORD=password psql -h localhost -p ${N3_PORT} -U admin ${DB_ID} -c \"INSERT INTO example (id, data) VALUES (2, 'Hello from n3!');\""

prompt_run "PGPASSWORD=password psql -h localhost -p ${N1_PORT} -U admin ${DB_ID} -c \"SELECT * FROM example;\""

info "Both rows replicated to n1 -- every node can read every other node's writes."

# ── Step 4: Resilience ───────────────────────────────────────────────────────

header "Step 4: Resilience"

explain "Active-active means every node accepts reads and writes. If a node"
explain "goes down, the others keep working -- and when it comes back, Spock"
explain "automatically catches it up."
explain ""
explain "Let's prove it. We'll halt n2 using Docker service scaling, write"
explain "data while it's down, then bring it back and verify everything"
explain "replicated."
explain ""
explain "Scaling the service to 0 cleanly stops n2 and prevents Control Plane"
explain "from auto-recovering it, so you can observe each step."
explain ""
prompt_continue

explain "Scale n2 to 0:"

prompt_run "N2_SERVICE=\$(docker service ls --filter label=pgedge.component=postgres --filter label=pgedge.node.name=n2 --format '{{ .Name }}') && docker service scale \"\$N2_SERVICE\"=0 && echo 'Node n2 scaled to 0.'"

explain "Write on n1 while n2 is down:"

prompt_run "PGPASSWORD=password psql -h localhost -p ${N1_PORT} -U admin ${DB_ID} -c \"INSERT INTO example (id, data) VALUES (3, 'Written while n2 is down!');\""

explain "Read from n3 to confirm the database still works:"

prompt_run "PGPASSWORD=password psql -h localhost -p ${N3_PORT} -U admin ${DB_ID} -c \"SELECT * FROM example;\""

info "The database kept working with a node down."
echo ""
explain "Now let's bring n2 back by scaling the service to 1:"

prompt_run "N2_SERVICE=\$(docker service ls --filter label=pgedge.component=postgres --filter label=pgedge.node.name=n2 --format '{{ .Name }}') && docker service scale \"\$N2_SERVICE\"=1 && echo 'Node n2 scaling back up.'"

start_spinner "Waiting for n2 container to come back..."
retries=60
while [[ "$retries" -gt 0 ]]; do
  if docker ps --filter label=pgedge.node.name=n2 --format '{{.Names}}' | grep -q .; then
    break
  fi
  sleep 3
  retries=$((retries - 1))
done
stop_spinner

if [[ "$retries" -eq 0 ]]; then
  warn "n2 did not come back within 3 minutes. You can check status with:"
  show_cmd "docker ps --filter label=pgedge.node.name=n2 --format '{{.Names}}'"
else
  info "n2 is back! Waiting for Postgres to accept connections..."
  n2_retries=20
  while [[ "$n2_retries" -gt 0 ]]; do
    if PGPASSWORD=password psql -h localhost -p "${N2_PORT}" -U admin "${DB_ID}" \
      -c "SELECT 1;" >/dev/null 2>&1; then
      break
    fi
    sleep 2
    n2_retries=$((n2_retries - 1))
  done
  if [[ "$n2_retries" -eq 0 ]]; then
    warn "n2 container is back but Postgres may still be starting."
  fi
  info "Waiting for replication to sync..."
  sync_retries=20
  while [[ "$sync_retries" -gt 0 ]]; do
    if PGPASSWORD=password psql -h localhost -p "${N2_PORT}" -U admin "${DB_ID}" \
      -tAc "SELECT 1 FROM example WHERE id = 3;" 2>/dev/null | grep -qx '1'; then
      break
    fi
    sleep 3
    sync_retries=$((sync_retries - 1))
  done
  if [[ "$sync_retries" -eq 0 ]]; then
    warn "Replication may not have fully synced yet."
  fi

  explain ""
  explain "Let's read from n2. Everything should be there -- including the row"
  explain "that was written while n2 was down:"

  prompt_run "PGPASSWORD=password psql -h localhost -p ${N2_PORT} -U admin ${DB_ID} -c \"SELECT * FROM example;\""

  info "The database survived a node failure, n2 came back via service"
  info "scaling, and Spock replication caught everything up. Zero data loss."
fi

prompt_continue

# ── Completion ───────────────────────────────────────────────────────────────

header "Done!"

info "You've created a distributed Postgres database, verified multi-master"
info "replication, and proven automatic recovery from node failure --"
info "all through the Control Plane API."
echo ""
explain "What's next:"
explain ""
explain "  Control Plane docs: https://docs.pgedge.com/control-plane/"
explain "  Full walkthrough:   https://docs.pgedge.com/control-plane/walkthrough"
echo ""
explain "${BOLD}To clean up:${RESET}"
explain ""
explain "  ${DIM}# Remove database services (stops Postgres containers)${RESET}"
explain "  ${DIM}docker service rm \$(docker service ls --filter label=pgedge.database.id=${DB_ID} -q)${RESET}"
explain "  ${DIM}# Remove the Control Plane container${RESET}"
explain "  ${DIM}docker rm -f ${CP_CONTAINER}${RESET}"
explain "  ${DIM}# Remove the data directory${RESET}"
explain "  ${DIM}sudo rm -rf ${CP_DATA}${RESET}"
echo ""
