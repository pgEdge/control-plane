#!/usr/bin/env bash

set -o errexit
set -o pipefail

# if [[ ! -e /etc/pgedge-control-plane/config.json ]]; then
#     cat <<EOF > /etc/pgedge-control-plane/config.json
# {
#   "orchestrator": "systemd",
#   "ipv4_address": "$(hostname -I | cut -d' ' -f1)",
#   "host_id": "$(hostname -s)",
#   "data_dir": "/var/lib/pgedge-control-plane"
# }
# EOF
# fi

# if command -v semodule &>/dev/null && selinuxenabled 2>/dev/null; then
#     semodule -i /usr/share/selinux/packages/pgedge-control-plane/pgedge-control-plane.pp
# fi
