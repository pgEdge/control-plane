##############################
# base image for all flavors #
##############################

FROM rockylinux/rockylinux:9-ubi AS base

ARG PACKAGE_RELEASE_CHANNEL=""
ARG POSTGRES_USER_ID="1020"

RUN <<EOF
#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

useradd -u ${POSTGRES_USER_ID} -m postgres -s /bin/bash

dnf install -y epel-release dnf
dnf config-manager --set-enabled crb
dnf update -y --allowerasing
dnf install -y https://dnf.pgedge.com/reporpm/pgedge-release-latest.noarch.rpm
if [[ -n "${PACKAGE_RELEASE_CHANNEL}" ]]; then
    sed -i "s|release|${PACKAGE_RELEASE_CHANNEL}|g" /etc/yum.repos.d/pgedge.repo
fi

EOF

##########################
# minimal-flavored image #
##########################

FROM base AS minimal

ARG PACKAGE_LIST_FILE
ARG TARGETARCH
ARG POSTGRES_MAJOR_VERSION

COPY packagelists/${TARGETARCH}/${PACKAGE_LIST_FILE} /usr/share/pgedge/packages.txt

RUN <<EOF
#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

xargs dnf install -y < /usr/share/pgedge/packages.txt
dnf clean all

EOF

USER postgres

ENV PG_MAJOR=${POSTGRES_MAJOR_VERSION}
ENV PATH=$PATH:/usr/pgsql-${POSTGRES_MAJOR_VERSION}/bin

# TODO: this entrypoint is temporary, pending PLAT-251
ENTRYPOINT ["/usr/pgsql-${POSTGRES_MAJOR_VERSION}/bin/postgres"]

###########################
# standard-flavored image #
###########################

FROM base AS standard

ARG PACKAGE_LIST_FILE
ARG TARGETARCH
ARG POSTGRES_MAJOR_VERSION

COPY packagelists/${TARGETARCH}/${PACKAGE_LIST_FILE} /usr/share/pgedge/packages.txt

RUN <<EOF
#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

xargs dnf install -y < /usr/share/pgedge/packages.txt
dnf install -y python3-pip-21.3.1-1.el9
pip install 'patroni[etcd,jsonlogger]==4.0.5'
dnf remove -y python3-pip
dnf clean all

EOF

USER postgres

ENV PG_MAJOR=${POSTGRES_MAJOR_VERSION}
ENV PATH=$PATH:/usr/pgsql-${POSTGRES_MAJOR_VERSION}/bin

# TODO: this entrypoint is temporary, pending PLAT-251
ENTRYPOINT ["/usr/local/bin/patroni"]
