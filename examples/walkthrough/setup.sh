#!/usr/bin/env bash
set -euo pipefail

# setup.sh -- Check prerequisites for the Control Plane walkthrough.
# Run this before guide.sh to verify your environment is ready.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=runner.sh
source "$SCRIPT_DIR/runner.sh"
OS="$(uname -s)"

# ── Prerequisite checks ─────────────────────────────────────────────

header "Control Plane Walkthrough -- Prerequisites Check"

explain "Checking that required tools are installed..."
echo ""

REQUIRED_CMDS=(docker curl jq)
MISSING=()

for cmd in "${REQUIRED_CMDS[@]}"; do
  if command -v "$cmd" &>/dev/null; then
    info "$cmd  found ($(command -v "$cmd"))"
  else
    error "$cmd  not found"
    MISSING+=("$cmd")
  fi
done

echo ""

if [[ ${#MISSING[@]} -gt 0 ]]; then
  warn "Missing tools: ${MISSING[*]}"
  echo ""
  explain "Install hints:"

  for cmd in "${MISSING[@]}"; do
    case "$cmd" in
      docker)
        explain "  docker  -- https://docs.docker.com/get-docker/"
        ;;
      curl)
        explain "  curl    -- https://curl.se/download.html"
        ;;
      jq)
        explain "  jq      -- https://jqlang.github.io/jq/download/"
        ;;
    esac
  done

  echo ""
  error "Install the missing tools, then re-run:"
  explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  echo ""
  exit 1
fi

# ── Verify Docker daemon is accessible ────────────────────────────────

explain "Verifying Docker daemon is accessible..."

if ! docker info &>/dev/null; then
  echo ""
  if [[ "$OS" == "Darwin" ]]; then
    error "Docker does not appear to be running."
    explain ""
    explain "Open Docker Desktop and wait for it to start, then re-run:"
    explain ""
    explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  elif command -v systemctl &>/dev/null; then
    # Distinguish "not running" from "no permission" without sudo prompts
    if systemctl is-active docker &>/dev/null 2>&1; then
      error "Docker is running but your user cannot access it."
      explain ""
      explain "Run these commands to fix permissions, then re-run the guide:"
      explain ""
      explain "  ${DIM}sudo usermod -aG docker \$USER${RESET}"
      explain "  ${DIM}newgrp docker${RESET}"
      explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
    else
      error "Docker is installed but the daemon is not running."
      explain ""
      explain "Run these commands to start Docker, then re-run the guide:"
      explain ""
      explain "  ${DIM}sudo systemctl daemon-reload${RESET}"
      explain "  ${DIM}sudo systemctl start docker${RESET}"
      explain "  ${DIM}sudo systemctl enable docker${RESET}"
      explain "  ${DIM}sudo usermod -aG docker \$USER${RESET}"
      explain "  ${DIM}newgrp docker${RESET}"
      explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
    fi
  else
    error "Cannot connect to the Docker daemon."
    explain ""
    explain "Make sure Docker is installed and running, then re-run:"
    explain ""
    explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
  fi
  exit 1
fi

info "Docker daemon is running."
echo ""

# ── Verify host networking (macOS Docker Desktop) ─────────────────────

if [[ "$OS" == "Darwin" ]]; then
  explain "Checking Docker Desktop host networking..."
  # Start a throwaway TCP listener with --network host and verify the
  # port is reachable from the Mac host. "alpine true" only checks that
  # Docker accepts the flag, not that ports are actually forwarded.
  HOST_NET_OK=false
  HOST_NET_PORT=19876
  if docker run --rm -d --network host --name pgedge-hostnet-check \
    alpine sh -c "nc -l -p $HOST_NET_PORT" &>/dev/null; then
    sleep 1
    if nc -z localhost "$HOST_NET_PORT" &>/dev/null; then
      HOST_NET_OK=true
    fi
    docker rm -f pgedge-hostnet-check >/dev/null 2>&1 || true
  fi
  if [[ "$HOST_NET_OK" != "true" ]]; then
    echo ""
    error "Host networking is not enabled in Docker Desktop."
    explain ""
    explain "Control Plane requires host networking. To enable it:"
    explain ""
    explain "  1. Open Docker Desktop"
    explain "  2. Go to ${BOLD}Settings > Resources > Network${RESET}"
    explain "  3. Check ${BOLD}Enable host networking${RESET}"
    explain "  4. Click ${BOLD}Apply and restart${RESET}"
    explain ""
    explain "  More info: https://docs.docker.com/engine/network/drivers/host/#docker-desktop"
    explain ""
    explain "Then re-run:"
    explain "  ${DIM}bash ${SCRIPT_DIR}/guide.sh${RESET}"
    exit 1
  fi
  info "Docker Desktop host networking is enabled."
  echo ""
fi

# ── Done ─────────────────────────────────────────────────────────────

info "All prerequisites satisfied. You are ready to run guide.sh."
