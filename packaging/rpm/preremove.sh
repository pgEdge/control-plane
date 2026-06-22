#!/bin/sh

if [ "$1" -eq 0 ]; then
    /bin/systemctl stop pgedge-control-plane.service >/dev/null 2>&1 || :
    /bin/systemctl --no-reload disable pgedge-control-plane.service >/dev/null 2>&1 || :
fi
