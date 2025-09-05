##############################
# base image for all flavors #
##############################

FROM rockylinux/rockylinux:9-ubi AS base

ARG PACKAGE_RELEASE_CHANNEL=""
ARG POSTGRES_USER_ID="1020"
COPY docker-entrypoint.sh docker-ensure-initdb.sh /usr/local/bin/

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

mkdir /docker-entrypoint-initdb.d

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

ENV PGDATA=/var/lib/pgsql/${POSTGRES_MAJOR_VERSION}/data
RUN install --verbose --directory --owner postgres --group postgres --mode 1777 "$PGDATA"

USER postgres

ENV PG_MAJOR=${POSTGRES_MAJOR_VERSION}
ENV PATH=$PATH:/usr/pgsql-${POSTGRES_MAJOR_VERSION}/bin

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# We set the default STOPSIGNAL to SIGINT, which corresponds to what PostgreSQL
# calls "Fast Shutdown mode" wherein new connections are disallowed and any
# in-progress transactions are aborted, allowing PostgreSQL to stop cleanly and
# flush tables to disk.
#
# See https://www.postgresql.org/docs/current/server-shutdown.html for more details
# about available PostgreSQL server shutdown signals.
#
# See also https://www.postgresql.org/docs/current/server-start.html for further
# justification of this as the default value, namely that the example (and
# shipped) systemd service files use the "Fast Shutdown mode" for service
# termination.
#
STOPSIGNAL SIGINT
#
# An additional setting that is recommended for all users regardless of this
# value is the runtime "--stop-timeout" (or your orchestrator/runtime's
# equivalent) for controlling how long to wait between sending the defined
# STOPSIGNAL and sending SIGKILL.
#
# The default in most runtimes (such as Docker) is 10 seconds, and the
# documentation at https://www.postgresql.org/docs/current/server-start.html notes
# that even 90 seconds may not be long enough in many instances.

EXPOSE 5432
CMD ["postgres"]

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

ENV PGDATA=/var/lib/pgsql/${POSTGRES_MAJOR_VERSION}/data
RUN install --verbose --directory --owner postgres --group postgres --mode 1777 "$PGDATA"

USER postgres

ENV PG_MAJOR=${POSTGRES_MAJOR_VERSION}
ENV PATH=$PATH:/usr/pgsql-${POSTGRES_MAJOR_VERSION}/bin

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# We set the default STOPSIGNAL to SIGINT, which corresponds to what PostgreSQL
# calls "Fast Shutdown mode" wherein new connections are disallowed and any
# in-progress transactions are aborted, allowing PostgreSQL to stop cleanly and
# flush tables to disk.
#
# See https://www.postgresql.org/docs/current/server-shutdown.html for more details
# about available PostgreSQL server shutdown signals.
#
# See also https://www.postgresql.org/docs/current/server-start.html for further
# justification of this as the default value, namely that the example (and
# shipped) systemd service files use the "Fast Shutdown mode" for service
# termination.
#
STOPSIGNAL SIGINT
#
# An additional setting that is recommended for all users regardless of this
# value is the runtime "--stop-timeout" (or your orchestrator/runtime's
# equivalent) for controlling how long to wait between sending the defined
# STOPSIGNAL and sending SIGKILL.
#
# The default in most runtimes (such as Docker) is 10 seconds, and the
# documentation at https://www.postgresql.org/docs/current/server-start.html notes
# that even 90 seconds may not be long enough in many instances.

EXPOSE 5432
CMD ["postgres"]