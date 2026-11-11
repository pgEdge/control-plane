# Rocky 9 host for the systemd E2E fixture: systemd as PID 1, the pgEdge
# prerequisites, and the locally built Control Plane package.
FROM rockylinux/rockylinux:9

ENV container=docker

RUN dnf install -y systemd systemd-sysv sudo && \
    (cd /lib/systemd/system/sysinit.target.wants/ && \
     for i in *; do [ "$i" = systemd-tmpfiles-setup.service ] || rm -f "$i"; done) && \
    rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/sockets.target.wants/*initctl* \
          /lib/systemd/system/basic.target.wants/* && \
    sed -i '1a account    sufficient   pam_rootok.so' /etc/pam.d/sudo && \
    dnf clean all

# pgEdge prerequisites from the staging repo. 
RUN dnf install -y --setopt=retries=10 epel-release && \
    dnf config-manager --set-enabled crb && \
    dnf install -y --nogpgcheck https://dnf.pgedge.com/reporpm/pgedge-release-latest.noarch.rpm && \
    sed -i 's/release/staging/g' /etc/yum.repos.d/pgedge.repo && \
    dnf install -y --setopt=retries=10 \
      pgedge-postgresql18 \
      pgedge-spock50_18 \
      pgedge-postgresql18-contrib \
      pgedge-pgbackrest \
      pgedge-patroni \
      pgedge-postgrest \
      pgedge-postgres-mcp && \
    dnf clean all

# Locally built CP package, copying and installing
COPY pgedge-control-plane.rpm /tmp/pgedge-control-plane.rpm
RUN rpm -ivh /tmp/pgedge-control-plane.rpm && \
    rm /tmp/pgedge-control-plane.rpm && \
    systemctl enable pgedge-control-plane

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
