#!/usr/bin/env bash
set -euo pipefail

echo "=== pgEdge Control Plane Walkthrough — Codespaces Setup ==="

# Install jq (not in base Ubuntu devcontainer)
echo "Installing jq..."
sudo apt-get update -qq
sudo apt-get install -y -qq jq >/dev/null 2>&1

# Add the pgEdge repository and install the Postgres client
echo "Installing pgEdge Postgres client..."
sudo curl -sSL https://apt.pgedge.com/repodeb/pgedge-release_latest_all.deb -o /tmp/pgedge-release.deb
sudo dpkg -i /tmp/pgedge-release.deb
rm -f /tmp/pgedge-release.deb
sudo apt-get update -qq
sudo apt-get install -y -qq pgedge-postgresql-client-18 >/dev/null 2>&1

# Run the prerequisites check
echo ""
bash examples/walkthrough/setup.sh

echo ""
echo "Setup complete!"
echo "  Interactive Guide: bash examples/walkthrough/guide.sh"
echo "  Walkthrough:       Open docs/walkthrough.md (Runme extension installed)"
