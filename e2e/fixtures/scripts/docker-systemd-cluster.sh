#!/bin/bash
# docker-systemd-cluster.sh — spin up a 3-node CP systemd cluster in Docker
#
# Usage:
#   ./e2e/fixtures/scripts/docker-systemd-cluster.sh [--os rocky-9|debian-13] [up|down]
#
# Options:
#   --os   OS image: rocky-9 (default) | debian-13
#
# Prerequisites:
#   - Docker installed and running
#   - CP packages built: make goreleaser-test-release
#
# Rocky 9 cluster runs on localhost ports 3000-3002.
# Debian 13 cluster runs on localhost ports 3010-3012.
# E2E tests:
#   make test-e2e E2E_FIXTURE=docker-systemd-rocky9
#   make test-e2e E2E_FIXTURE=docker-systemd-debian13

set -euo pipefail

OS="rocky-9"
ACTION="up"

while [[ $# -gt 0 ]]; do
  case $1 in
    --os) OS="$2"; shift 2 ;;
    up|down) ACTION="$1"; shift ;;
    *) echo "Unknown argument: $1"; exit 1 ;;
  esac
done

case "$OS" in
  rocky-9)
    OS_FAMILY="rpm"
    IMAGE_TAG="cp-systemd-rocky9"
    NETWORK_NAME="cp-systemd-rocky9-net"
    SUBNET="172.30.0.0/24"
    CONTAINERS=("cp-rocky9-host-1" "cp-rocky9-host-2" "cp-rocky9-host-3")
    API_PORTS=(3000 3001 3002)
    FIXTURE_NAME="docker-systemd-rocky9"
    ;;
  debian-13)
    OS_FAMILY="deb"
    IMAGE_TAG="cp-systemd-debian13"
    NETWORK_NAME="cp-systemd-debian13-net"
    SUBNET="172.31.0.0/24"
    CONTAINERS=("cp-debian13-host-1" "cp-debian13-host-2" "cp-debian13-host-3")
    API_PORTS=(3010 3011 3012)
    FIXTURE_NAME="docker-systemd-debian13"
    ;;
  *) echo "Unsupported OS: $OS (use rocky-9 or debian-13)"; exit 1 ;;
esac

HOST_IDS=("host-1" "host-2" "host-3")

# Arch detection
HOST_ARCH=$(uname -m)
RPM_ARCH="$HOST_ARCH"
[[ "$RPM_ARCH" == "arm64" ]] && RPM_ARCH="aarch64"
DEB_ARCH="$HOST_ARCH"
[[ "$DEB_ARCH" == "x86_64" ]] && DEB_ARCH="amd64"

# ── Teardown ───────────────────────────────────────────────────────────────────
teardown() {
  echo "==> Tearing down $OS cluster..."
  for c in "${CONTAINERS[@]}"; do
    docker rm -f "$c" 2>/dev/null && echo "  Removed $c" || true
  done
  docker network rm "$NETWORK_NAME" 2>/dev/null && echo "  Removed network $NETWORK_NAME" || true
  echo "Done."
}

if [[ "$ACTION" == "down" ]]; then
  teardown
  exit 0
fi

# ── Verify prerequisites ───────────────────────────────────────────────────────
if [[ "$OS_FAMILY" == "rpm" ]]; then
  PKG=$(ls dist/pgedge-control-plane-*.el9."${RPM_ARCH}".rpm 2>/dev/null | sort -V | tail -1)
  if [[ -z "$PKG" ]]; then
    echo "ERROR: No CP RPM found in dist/ for el9.${RPM_ARCH}"
    echo "Run 'make goreleaser-test-release' first."
    exit 1
  fi
else
  PKG=$(ls dist/pgedge-control-plane_*.trixie_"${DEB_ARCH}".deb 2>/dev/null | sort -V | tail -1)
  if [[ -z "$PKG" ]]; then
    echo "ERROR: No CP DEB found in dist/ for trixie_${DEB_ARCH}"
    echo "Run 'make goreleaser-test-release' first."
    exit 1
  fi
fi
echo "==> Using CP package: $PKG"

# ── Build Docker image ─────────────────────────────────────────────────────────
build_image_rocky9() {
  echo "==> Building Rocky 9 systemd image..."
  docker build -t "$IMAGE_TAG" - <<'DOCKERFILE'
FROM rockylinux/rockylinux:9

ENV container=docker

RUN dnf install -y systemd systemd-sysv && \
    (cd /lib/systemd/system/sysinit.target.wants/ && \
     for i in *; do [ "$i" = systemd-tmpfiles-setup.service ] || rm -f "$i"; done) && \
    rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/sockets.target.wants/*initctl* \
          /lib/systemd/system/basic.target.wants/* && \
    dnf install -y jq sudo && \
    # The E2E fixture runs commands as root via sudo, which can fail PAM's
    # account check in this container, so let root skip it.
    sed -i '1a account    sufficient   pam_rootok.so' /etc/pam.d/sudo && \
    dnf clean all

VOLUME ["/sys/fs/cgroup"]
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
DOCKERFILE
}

build_image_debian13() {
  echo "==> Building Debian 13 systemd image..."
  docker build -t "$IMAGE_TAG" - <<'DOCKERFILE'
FROM debian:trixie

ENV container=docker
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends systemd systemd-sysv jq curl sudo && \
    rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/sockets.target.wants/*initctl* \
          /lib/systemd/system/basic.target.wants/* && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

VOLUME ["/sys/fs/cgroup"]
STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]
DOCKERFILE
}

if [[ "$OS_FAMILY" == "rpm" ]]; then
  build_image_rocky9
else
  build_image_debian13
fi

# ── Create network ─────────────────────────────────────────────────────────────
echo "==> Creating Docker network $NETWORK_NAME..."
docker network create --subnet="$SUBNET" "$NETWORK_NAME" 2>/dev/null || \
  echo "  Network $NETWORK_NAME already exists, reusing."

# ── Launch containers ──────────────────────────────────────────────────────────
echo "==> Launching containers..."
for i in 1 2 3; do
  c="${CONTAINERS[$((i-1))]}"
  port="$((API_PORTS[0] + i - 1))"
  docker rm -f "$c" 2>/dev/null || true
  docker run -d \
    --name "$c" \
    --hostname "host-$i" \
    --privileged \
    --cgroupns host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
    --tmpfs /run \
    --network "$NETWORK_NAME" \
    --dns 8.8.8.8 \
    -p "${port}:3000" \
    "$IMAGE_TAG"
  echo "  Started $c on localhost:${port}"
done

# ── Wait for systemd ───────────────────────────────────────────────────────────
wait_for_systemd() {
  local c=$1
  echo -n "  Waiting for systemd in $c..."
  # Wait for /run/systemd/notify socket to exist (ready once systemd is up, no D-Bus needed)
  for _ in $(seq 1 40); do
    if docker exec "$c" test -S /run/systemd/notify 2>/dev/null; then
      echo " ready"
      return 0
    fi
    sleep 2
    echo -n "."
  done
  echo " FAILED"
  docker exec "$c" journalctl --no-pager -n 30 || true
  return 1
}

echo "==> Waiting for systemd..."
for c in "${CONTAINERS[@]}"; do
  wait_for_systemd "$c"
done

# ── Install pgEdge prerequisites ───────────────────────────────────────────────
install_prereqs_rpm() {
  local c=$1
  docker exec "$c" bash -c "
    set -e
    dnf install -y epel-release
    dnf config-manager --set-enabled crb
    dnf install -y https://dnf.pgedge.com/reporpm/pgedge-release-latest.noarch.rpm --nogpgcheck
    sed -i 's/release/staging/g' /etc/yum.repos.d/pgedge.repo
    dnf install -y \
      pgedge-postgresql18 \
      pgedge-spock50_18 \
      pgedge-postgresql18-contrib \
      pgedge-pgbackrest \
      pgedge-patroni
  "
}

install_prereqs_deb() {
  local c=$1
  docker exec "$c" bash -c "
    set -e
    apt-get update
    apt-get install -y ca-certificates curl gnupg2 lsb-release
    curl -fsSL https://apt.pgedge.com/repodeb/pgedge-release_latest_all.deb -o /tmp/pgedge-release.deb
    dpkg -i /tmp/pgedge-release.deb
    sed -i 's/release/staging/g' /etc/apt/sources.list.d/pgedge.sources
    apt-get update
    apt-get install -y \
      pgedge-postgresql-18 \
      pgedge-postgresql-18-spock50 \
      pgedge-pgbackrest \
      pgedge-patroni
  "
}

# Package mirrors are occasionally inconsistent mid-sync, so retry with fresh
# metadata before giving up.
install_prereqs() {
  local c=$1
  for attempt in 1 2 3; do
    if [[ "$OS_FAMILY" == "rpm" ]]; then
      install_prereqs_rpm "$c" && return 0
      docker exec "$c" dnf clean all || true
    else
      install_prereqs_deb "$c" && return 0
      docker exec "$c" bash -c "apt-get clean && rm -rf /var/lib/apt/lists/*" || true
    fi
    echo "  Install attempt $attempt failed in $c, retrying in 15s..."
    sleep 15
  done
  echo "ERROR: Failed to install prerequisites in $c"
  return 1
}

echo "==> Installing pgEdge prerequisites..."
for c in "${CONTAINERS[@]}"; do
  echo "  Installing in $c..."
  install_prereqs "$c"
done

# ── Install Control Plane ──────────────────────────────────────────────────────
echo "==> Installing CP package in containers..."
for c in "${CONTAINERS[@]}"; do
  if [[ "$OS_FAMILY" == "rpm" ]]; then
    docker cp "$PKG" "$c:/root/pgedge-control-plane.rpm"
    docker exec "$c" rpm -ivh /root/pgedge-control-plane.rpm
  else
    docker cp "$PKG" "$c:/root/pgedge-control-plane.deb"
    docker exec "$c" dpkg -i /root/pgedge-control-plane.deb
  fi
  echo "  Installed CP in $c"
done

# ── Configure and start CP ─────────────────────────────────────────────────────
echo "==> Configuring CP..."
for i in "${!CONTAINERS[@]}"; do
  c="${CONTAINERS[$i]}"
  host_id="${HOST_IDS[$i]}"
  ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$c")

  echo "  $c -> $ip ($host_id)"
  docker exec "$c" bash -c "
    mkdir -p /etc/pgedge-control-plane
    printf '{\"orchestrator\":\"systemd\",\"data_dir\":\"/var/lib/pgedge-control-plane\",\"host_id\":\"${host_id}\",\"peer_addresses\":[\"${ip}\"],\"client_addresses\":[\"${ip}\"]}' \
      > /etc/pgedge-control-plane/config.json
  "
  docker exec "$c" systemctl enable pgedge-control-plane
  docker exec "$c" systemctl start pgedge-control-plane
done

# ── Wait for CP APIs ───────────────────────────────────────────────────────────
echo "==> Waiting for CP APIs..."
for port in "${API_PORTS[@]}"; do
  echo -n "  localhost:${port}..."
  for _ in $(seq 1 40); do
    if curl -sf "http://localhost:${port}/v1/version" > /dev/null 2>&1; then
      echo " ready"
      break
    fi
    sleep 3
    echo -n "."
  done
done

# ── Initialize cluster ─────────────────────────────────────────────────────────
echo "==> Initializing cluster on host-1..."
RESPONSE=$(curl -sf "http://localhost:${API_PORTS[0]}/v1/cluster/init")
TOKEN=$(echo "$RESPONSE" | jq -r '.token')
URLS=$(echo "$RESPONSE" | jq -c '.server_urls')

if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  echo "ERROR: Cluster init failed. Response: $RESPONSE"
  exit 1
fi

echo "==> Joining host-2 and host-3..."
for i in 1 2; do
  curl -sf -X POST "http://localhost:${API_PORTS[$i]}/v1/cluster/join" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$TOKEN\",\"server_urls\":$URLS}" > /dev/null
  echo "  ${HOST_IDS[$i]} joined"
done

# ── Wait for all hosts healthy ─────────────────────────────────────────────────
echo "==> Waiting for all hosts to be healthy..."
HEALTHY=0
for _ in $(seq 1 60); do
  HEALTHY=$(curl -sf "http://localhost:${API_PORTS[0]}/v1/hosts" \
    | jq '[.hosts[] | select(.status.state == "healthy")] | length' 2>/dev/null || echo 0)
  if [[ "$HEALTHY" -eq "${#CONTAINERS[@]}" ]]; then
    echo "  All ${#CONTAINERS[@]} hosts healthy."
    break
  fi
  echo "  $HEALTHY/${#CONTAINERS[@]} healthy, waiting..."
  sleep 5
done

if [[ "$HEALTHY" -lt "${#CONTAINERS[@]}" ]]; then
  echo "ERROR: Not all hosts became healthy in time."
  exit 1
fi

# ── Verify sudo ────────────────────────────────────────────────────────────────
# The E2E fixture runs host commands through sudo, so fail fast if it's broken.
echo "==> Verifying sudo works in containers..."
for c in "${CONTAINERS[@]}"; do
  if ! docker exec "$c" sh -c "sudo -u postgres true"; then
    echo "ERROR: sudo failed in $c"
    exit 1
  fi
done

# ── Write E2E test config ──────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUTS_DIR="${SCRIPT_DIR}/../outputs"
TEST_CONFIG="${OUTPUTS_DIR}/${FIXTURE_NAME}.test_config.yaml"

echo "==> Writing E2E test config to ${TEST_CONFIG}..."
mkdir -p "$OUTPUTS_DIR"
{
  echo "---"
  echo "hosts:"
  for i in "${!CONTAINERS[@]}"; do
    echo "  ${HOST_IDS[$i]}:"
    echo "    external_ip: 127.0.0.1"
    echo "    port: ${API_PORTS[$i]}"
    # The E2E fixture appends the command to run as a single argument
    echo "    ssh_command: docker exec ${CONTAINERS[$i]} sh -c"
  done
} > "$TEST_CONFIG"

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "$OS cluster ready!"
for i in "${!CONTAINERS[@]}"; do
  echo "  ${HOST_IDS[$i]}: http://localhost:${API_PORTS[$i]}"
done
echo ""
echo "  Run E2E tests:"
echo "    make test-e2e E2E_FIXTURE=${FIXTURE_NAME}"
echo ""
echo "  Tear down:"
echo "    $0 --os $OS down"
