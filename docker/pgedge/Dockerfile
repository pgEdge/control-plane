FROM rockylinux/rockylinux:9.5-ubi

ARG REPO_BASE_URL
ARG RELEASE_CHANNEL
ARG POSTGRES_VERSION
ARG IMAGE_VERSION
ARG POSTGRES_USER_ID="1020"

COPY packagelists/pg${POSTGRES_VERSION}_${IMAGE_VERSION}.txt /usr/share/pgedge/packages.txt

RUN <<EOF
#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

useradd -u ${POSTGRES_USER_ID} -m postgres -s /bin/bash

curl -Ls ${REPO_BASE_URL}/repos/yum/install-repos.sh \
    | REPO_BASE_URL=${REPO_BASE_URL} \
        RELEASE_CHANNEL=${RELEASE_CHANNEL} \
        POSTGRES_VERSION=${POSTGRES_VERSION} \
        bash

dnf install -y epel-release dnf
dnf config-manager --set-enabled crb
dnf update -y --allowerasing
xargs dnf install -y < /usr/share/pgedge/packages.txt
pip install 'python-json-logger==3.2.1'
dnf remove -y python3-pip
dnf clean all

EOF

USER postgres

ENV PG_MAJOR=${POSTGRES_VERSION}
ENV PATH=$PATH:/usr/pgsql-${POSTGRES_VERSION}/bin

ENTRYPOINT ["/usr/bin/patroni"]
