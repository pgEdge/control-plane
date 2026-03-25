# Installing the pgEdge Control Plane via System Packages

!!! warning "Preview Feature"

    System package-based installation is a preview feature. Not all Control Plane
    features are supported, and some aspects of this
    installation method are subject to change. We do not recommend it for
    production environments yet. We'd love your feedback - please share your
    experience in our [GitHub issues](https://github.com/pgedge/control-plane/issues)
    or join our [Discord](https://discord.com/invite/pgedge/login).

This guide covers installing the pgEdge Control Plane on RPM-based Linux hosts
(e.g. RHEL, Rocky Linux, AlmaLinux) using the RPM package attached to each
[GitHub release](https://github.com/pgedge/control-plane/releases). Support for
Debian-based hosts is coming in a future release.

Unlike the Docker Swarm installation method, the system package installation
runs the Control Plane directly on the host. The Control Plane will use systemd
to manage Postgres instances rather than Docker containers.

## Prerequisites

### Ports

By default, the Control Plane uses these ports, which must be accessible on each
machine by other cluster members:

  - Port `3000` TCP for HTTP communication
  - Port `2379` TCP for Etcd peer communication
  - Port `2380` TCP for Etcd client communication

You can configure alternate ports by modifying the
[configuration file](#configuration) after installing the `pgedge-control-plane`
RPM.

### Packages

The Control Plane depends on the pgEdge Enterprise Postgres Packages. It does
not yet install Postgres or its supporting packages automatically. You must
install them on each host before starting the Control Plane.

Run the following on each host as root:

```sh
# Install prerequisites for the pgEdge Enterprise Postgres packages
dnf install -y epel-release dnf
dnf config-manager --set-enabled crb

# Install the pgEdge Enterprise Postgres repository
dnf install -y https://dnf.pgedge.com/reporpm/pgedge-release-latest.noarch.rpm

# Install the required packages for your Postgres version. We currently support
# versions 16, 17, and 18. Set postgres_major_version to your desired version.
POSTGRES_MAJOR_VERSION='<16|17|18>'
dnf install -y \
      pgedge-postgresql${POSTGRES_MAJOR_VERSION} \
      pgedge-spock50_${POSTGRES_MAJOR_VERSION} \
      pgedge-postgresql${POSTGRES_MAJOR_VERSION}-contrib \
      pgedge-pgbackrest \
      pgedge-python3-psycopg2

# Install Patroni
dnf install -y python3-pip
pip install 'patroni[etcd,jsonlogger]==4.1.0'
```

## Installing the RPM

We publish RPMs with our releases on the
[GitHub releases page](https://github.com/pgedge/control-plane/releases). RPMs
are available for both `amd64` and `arm64`.

Install the RPM with:

```sh
# Detect architecture
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Set the version to install
VERSION="v0.7.0"

# Download the RPM
curl -LO "https://github.com/pgedge/control-plane/releases/download/${VERSION}/pgedge-control-plane_${VERSION#v}_linux_${ARCH}.rpm"

# Install the RPM
rpm -i pgedge-control-plane_${VERSION#v}_linux_${ARCH}.rpm
```

The RPM installs:

- `/usr/sbin/pgedge-control-plane` - the Control Plane binary
- `/usr/lib/systemd/system/pgedge-control-plane.service` - the systemd service unit
- `/etc/pgedge-control-plane/config.json` - the default configuration file

## Configuration

The default configuration file is located at
`/etc/pgedge-control-plane/config.json`:

```json
{
  "orchestrator": "systemd",
  "data_dir": "/var/lib/pgedge-control-plane"
}
```

The `orchestrator` field must be set to `"systemd"` for this installation
method. The `data_dir` is where the Control Plane stores its state, including
the embedded Etcd data.

The host ID defaults to the machine's short hostname. To set an explicit host
ID, add a `host_id` field to the config file:

```json
{
  "orchestrator": "systemd",
  "data_dir": "/var/lib/pgedge-control-plane",
  "host_id": "my-host-1"
}
```

You can find the full list of configuration settings in the
[Configuration reference](./configuration.md).

## Starting the Control Plane

Start and enable the Control Plane service:

```sh
systemctl enable --now pgedge-control-plane.service
```

To check the service status:

```sh
systemctl status pgedge-control-plane.service
```

To tail the logs:

```sh
journalctl -u pgedge-control-plane.service --follow
```

## Initializing the Control Plane

Once the service is running on all hosts, initialize and join them the same way
as a Docker Swarm installation.

Initialize the cluster on the first host:

```sh
curl http://localhost:3000/v1/cluster/init
```

The response contains a join token and server URL:

```json
{
  "token": "PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
  "server_urls": ["http://198.19.249.2:3000"]
}
```

Join each additional host to the cluster by submitting a `POST` request to that
host's `/v1/cluster/join` endpoint with the token and server URL from the
previous step:

```sh
curl -i -X POST http://<host_ip>:3000/v1/cluster/join \
    -H 'Content-Type: application/json' \
    --data '{
        "token": "PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
        "server_urls": ["http://198.19.249.2:3000"]
    }'
```

Repeat for each host. Once all hosts have joined, you can interact with the API
from any host in the cluster.

## Updating the Control Plane

To update to a newer version, download the new RPM from the
[GitHub releases page](https://github.com/pgedge/control-plane/releases) and
run:

```sh
systemctl stop pgedge-control-plane.service
rpm -U pgedge-control-plane-<new-version>.<arch>.rpm
systemctl start pgedge-control-plane.service
```

!!! note

    The RPM upgrade (`rpm -U`) preserves your existing configuration file at
    `/etc/pgedge-control-plane/config.json` because it is marked as a
    non-replaceable config file.
