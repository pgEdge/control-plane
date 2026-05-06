#!/bin/sh

/bin/systemctl stop pgedge-control-plane.service >/dev/null 2>&1 || :
/bin/systemctl --no-reload disable pgedge-control-plane.service >/dev/null 2>&1 || :
