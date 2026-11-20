# Rocky 9 host for the community systemd E2E fixture: systemd as PID 1, the
# community Postgres, pgBackRest, and Patroni packages from the PGDG repo
# (without Spock), and the locally built Control Plane package.
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

# Community prerequisites from the PGDG repo.
RUN dnf install -y --setopt=retries=10 \
      "https://download.postgresql.org/pub/repos/yum/reporpms/EL-9-$(uname -m)/pgdg-redhat-repo-latest.noarch.rpm" && \
    dnf install -y --setopt=retries=10 \
      postgresql18-server \
      postgresql18-contrib \
      pgbackrest \
      patroni \
      patroni-etcd && \
    dnf clean all

# Locally built CP package, copying and installing
COPY pgedge-control-plane.rpm /tmp/pgedge-control-plane.rpm
RUN rpm -ivh /tmp/pgedge-control-plane.rpm && \
    rm /tmp/pgedge-control-plane.rpm && \
    systemctl enable pgedge-control-plane

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
