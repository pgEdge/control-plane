#!/usr/bin/env bash
set -euo pipefail

echo "=== pgEdge Control Plane Walkthrough — Codespaces Setup ==="

# Add the pgEdge repository. downloads.pgedge.com, not apt.pgedge.com: the
# gated repository cannot serve the package that configures access to it.
echo "Adding pgEdge apt repository..."
sudo curl -sSL https://downloads.pgedge.com/repodeb/pgedge-release_latest_all.deb -o /tmp/pgedge-release.deb
sudo dpkg -i /tmp/pgedge-release.deb

# The repository ships disabled and authenticates clients with mutual TLS.
# Supply PGEDGE_CLIENT_CERT and PGEDGE_CLIENT_KEY (PEM contents) to use it.
if [ -n "${PGEDGE_CLIENT_CERT:-}" ] && [ -n "${PGEDGE_CLIENT_KEY:-}" ]; then
    echo "Installing the pgEdge client certificate..."
    sudo install -d -m 0755 /etc/pgedge
    # Create the key empty at 0600 first: writing it and then chmod'ing would
    # leave it world-readable for the interval in between.
    sudo install -m 0600 /dev/null /etc/pgedge/client.key
    sudo install -m 0644 /dev/null /etc/pgedge/client.crt
    printf '%s\n' "$PGEDGE_CLIENT_CERT" | sudo tee /etc/pgedge/client.crt >/dev/null
    printf '%s\n' "$PGEDGE_CLIENT_KEY"  | sudo tee /etc/pgedge/client.key >/dev/null
    sudo pgedge-subscription-manager install \
        /etc/pgedge/client.crt /etc/pgedge/client.key --force
else
    echo "WARNING: PGEDGE_CLIENT_CERT / PGEDGE_CLIENT_KEY are not set."
    echo "         The pgEdge repository stays disabled and the install below will fail."
fi

# Install jq and the Postgres client
echo "Installing jq and pgEdge Postgres client..."
sudo apt-get update -qq
sudo apt-get install -y -qq jq pgedge-postgresql-client-18
# Run the prerequisites check
echo ""
bash examples/walkthrough/setup.sh

echo ""
echo "Setup complete!"
echo "  Interactive Guide: bash examples/walkthrough/guide.sh"
echo "  Walkthrough:       Open docs/walkthrough.md (Runme extension installed)"
