# Debian 13 host for the community systemd E2E fixture: systemd as PID 1, the
# community Postgres, pgBackRest, and Patroni packages from the PGDG repo
# (without Spock), and the locally built Control Plane package.
FROM debian:trixie

ENV container=docker
ENV DEBIAN_FRONTEND=noninteractive

# Run systemd as PID 1, with only the units a container needs. curl is used
# by the healthcheck.
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

# Community prerequisites from the PGDG repo. The Control Plane manages its
# own clusters, so we prevent the Postgres package from creating a default
# one.
RUN apt-get update -o Acquire::Retries=5 && \
    apt-get install -y -o Acquire::Retries=5 postgresql-common && \
    sed -i 's/^#\?\s*create_main_cluster.*/create_main_cluster = false/' \
      /etc/postgresql-common/createcluster.conf && \
    /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y && \
    apt-get update -o Acquire::Retries=5 && \
    apt-get install -y -o Acquire::Retries=5 \
      postgresql-18 \
      pgbackrest \
      patroni \
      python3-etcd && \
    systemctl disable postgresql.service && \
    rm -rf /var/lib/apt/lists/*

# Locally built CP package, copying and installing
COPY pgedge-control-plane.deb /tmp/pgedge-control-plane.deb
RUN dpkg -i /tmp/pgedge-control-plane.deb && \
    rm /tmp/pgedge-control-plane.deb && \
    systemctl enable pgedge-control-plane

# systemd related setting for stopping container
STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]
