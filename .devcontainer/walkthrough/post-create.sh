#!/usr/bin/env bash
set -euo pipefail

echo "=== pgEdge Control Plane Walkthrough — Codespaces Setup ==="

# Install jq and psql (not in base Ubuntu devcontainer)
echo "Installing prerequisites..."
sudo apt-get update -qq
sudo apt-get install -y -qq jq postgresql-client >/dev/null 2>&1

# Run the prerequisites check
echo ""
bash examples/walkthrough/setup.sh

echo ""
echo "Setup complete!"
echo "  Interactive Guide: bash examples/walkthrough/guide.sh"
echo "  Walkthrough:       Open docs/walkthrough.md (Runme extension installed)"
