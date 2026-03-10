#!/usr/bin/env bash
set -euo pipefail

echo "=== pgEdge Control Plane Walkthrough — Codespaces Setup ==="

# Add the pgEdge repository
echo "Adding pgEdge apt repository..."
sudo curl -sSL https://apt.pgedge.com/repodeb/pgedge-release_latest_all.deb -o /tmp/pgedge-release.deb
sudo dpkg -i /tmp/pgedge-release.deb
rm -f /tmp/pgedge-release.deb

# Install jq and the Postgres client
echo "Installing jq and pgEdge Postgres client..."
sudo apt-get update -qq
sudo apt-get install -y -qq jq pgedge-postgresql-client-18

# Ensure psql is on PATH
if ! command -v psql &>/dev/null; then
  PSQL_BIN=$(find /usr/lib -name psql -type f 2>/dev/null | head -1)
  if [[ -n "$PSQL_BIN" ]]; then
    sudo ln -sf "$PSQL_BIN" /usr/local/bin/psql
    echo "Linked psql to /usr/local/bin/psql"
  else
    echo "Warning: psql not found after installation"
  fi
fi

# Run the prerequisites check
echo ""
bash examples/walkthrough/setup.sh

echo ""
echo "Setup complete!"
echo "  Interactive Guide: bash examples/walkthrough/guide.sh"
echo "  Walkthrough:       Open docs/walkthrough.md (Runme extension installed)"
