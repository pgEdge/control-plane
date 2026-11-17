# Debian 13 host image for the systemd E2E fixture for ci: installs systemd, the pgEdge
# prerequisites, and the locally built Control Plane package.
FROM debian:trixie

ENV container=docker
ENV DEBIAN_FRONTEND=noninteractive

# Run systemd as PID 1, with only the units a container needs. curl is used
# by the healthcheck and to fetch the pgEdge repo package.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      systemd systemd-sysv sudo curl ca-certificates gnupg2 lsb-release && \
    rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/sockets.target.wants/*initctl* \
          /lib/systemd/system/basic.target.wants/* && \
    rm -rf /var/lib/apt/lists/*

# pgEdge prerequisites from the release repo, so CI tests certified builds.
RUN curl -fsSL --retry 5 https://apt.pgedge.com/repodeb/pgedge-release_latest_all.deb \
      -o /tmp/pgedge-release.deb && \
    dpkg -i /tmp/pgedge-release.deb && \
    rm /tmp/pgedge-release.deb && \
    apt-get update -o Acquire::Retries=5 && \
    apt-get install -y -o Acquire::Retries=5 \
      pgedge-postgresql-18 \
      pgedge-postgresql-18-spock50 \
      pgedge-pgbackrest \
      pgedge-patroni \
      pgedge-postgrest && \
    rm -rf /var/lib/apt/lists/*

# Locally built CP package copying and installing
COPY pgedge-control-plane.deb /tmp/pgedge-control-plane.deb
RUN dpkg -i /tmp/pgedge-control-plane.deb && \
    rm /tmp/pgedge-control-plane.deb && \
    systemctl enable pgedge-control-plane

# systemd related setting for stopping container
STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]
