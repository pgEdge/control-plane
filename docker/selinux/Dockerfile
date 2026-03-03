FROM rockylinux:9 AS build

RUN dnf install -y \
    checkpolicy \
    policycoreutils

COPY pgedge-control-plane.te /

RUN <<EOF
    checkmodule -M -m -o /pgedge-control-plane.mod /pgedge-control-plane.te
    semodule_package -o /pgedge-control-plane.pp -m /pgedge-control-plane.mod
EOF

FROM scratch AS output
COPY --from=build /pgedge-control-plane.pp /
