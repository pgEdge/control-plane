#!/bin/sh

if [ "$1" = "configure" ] && [ -n "$2" ]; then
    # true during a package upgrade
    /bin/systemctl daemon-reload >/dev/null 2>&1 || :
    /bin/systemctl try-restart pgedge-control-plane.service >/dev/null 2>&1 || :
fi
