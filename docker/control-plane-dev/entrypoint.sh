#!/usr/bin/env bash

set -o errexit
set -o pipefail

: "${DEBUG=0}"

if [[ "${DEBUG}" == 1 ]]; then
    exec /go/bin/dlv \
        --listen=:2345 \
        --headless=true \
        --log=true \
        --log-output=debugger,debuglineerr,gdbwire,lldbout,rpc \
        --accept-multiclient \
        --api-version=2 \
        exec /control-plane \
        -- \
        run \
        --config-path /config.json \
        --logging.pretty
else
    exec /control-plane run \
        --config-path /config.json \
        --logging.pretty
fi
