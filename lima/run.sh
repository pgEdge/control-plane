#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

set_env() {
    case ${HOSTNAME} in
        lima-control-plane-dev-1)
            export PGEDGE_HOST_ID=host-1
            export PGEDGE_HTTP__PORT=3010
            export PGEDGE_ETCD_SERVER__PEER_PORT=2390
            export PGEDGE_ETCD_SERVER__CLIENT_PORT=2389
            ;;
        lima-control-plane-dev-2)
            export PGEDGE_HOST_ID=host-2
            export PGEDGE_HTTP__PORT=3011
            export PGEDGE_ETCD_SERVER__PEER_PORT=2490
            export PGEDGE_ETCD_SERVER__CLIENT_PORT=2489
            ;;
        lima-control-plane-dev-3)
            export PGEDGE_HOST_ID=host-3
            export PGEDGE_HTTP__PORT=3012
            export PGEDGE_ETCD_SERVER__PEER_PORT=2590
            export PGEDGE_ETCD_SERVER__CLIENT_PORT=2589
            ;;
        lima-control-plane-dev-4)
            export PGEDGE_HOST_ID=host-4
            export PGEDGE_HTTP__PORT=3013
            export PGEDGE_ETCD_MODE=client
            ;;
        lima-control-plane-dev-5)
            export PGEDGE_HOST_ID=host-5
            export PGEDGE_HTTP__PORT=3014
            export PGEDGE_ETCD_MODE=client
            ;;
        lima-control-plane-dev-6)
            export PGEDGE_HOST_ID=host-6
            export PGEDGE_HTTP__PORT=3015
            export PGEDGE_ETCD_MODE=client
            ;;
        *)
            echo "unrecognized hostname ${HOSTNAME}"
            exit 1
            ;;
    esac

    export PGEDGE_DATA_DIR="${LIMA_DIR}/data/${PGEDGE_HOST_ID}"
    export PGEDGE_SYSTEMD__INSTANCE_DATA_DIR="${LIMA_DIR}/data/${PGEDGE_HOST_ID}/instances"
    
}

if [[ $(whoami) != "root" ]]; then
    echo "this script must be run as root"
    exit 1
fi

# Copy the binary to another location so that we can safely rebuild on the host
# without disrupting the running server.
cp "${LIMA_DIR}/pgedge-control-plane" /usr/sbin

# Set environment variable configuration.
set_env

# The sed prefixes each output line with the host ID.
/usr/sbin/pgedge-control-plane run \
    --config-path "${LIMA_DIR}/config.json" \
    2>&1 | sed "s/^/[${PGEDGE_HOST_ID}] /"
