# Installing the pgEdge Control Plane via System Packages

!!! warning "Preview Feature"

    System package-based installation is a preview feature that is under active
    development. The core database management API is fully functional and tested,
    but some features are not yet supported (see [Limitations](#limitations) below).
    The installation method and upgrade process between releases may change before
    this feature is finalized. We'd love your feedback - please share your
    experience in our [GitHub issues](https://github.com/pgedge/control-plane/issues)
    or join our [Discord](https://discord.com/invite/pgedge/login).

This guide covers installing the pgEdge Control Plane on Linux hosts that use
the RPM Package Manager (RPM) package format (e.g. Red Hat Enterprise Linux
(RHEL), Rocky Linux, AlmaLinux) using the RPM package attached to each [GitHub
release](https://github.com/pgedge/control-plane/releases). Support for
Debian-based hosts is coming in a future release.

Unlike the Docker Swarm installation method, the system package installation
runs the Control Plane directly on the host. The Control Plane uses systemd to
manage Postgres instances rather than Docker containers.

## Limitations

The systemd installation method has the following known limitations in the
current release.

- Database upgrades are not supported via the API. Minor version upgrades can be
  performed manually by a system administrator. Further support for package
  management and database upgrades will be added in subsequent releases.
- Supporting Services are not yet supported on systemd clusters; support is
  coming in a subsequent release.
- All hosts in a cluster must use the same orchestrator (either `swarm` or
  `systemd`); the orchestrator must not change after the cluster is initialized.

## Prerequisites

The Control Plane requires specific ports and packages to be configured on each
host before installation.

### Ports

The Control Plane uses these ports by default; each must be accessible to other
cluster members on each host.

- Port `3000` uses TCP for HTTP and HTTPS communication.
- Port `2379` uses TCP for Etcd client communication.
- Port `2380` uses TCP for Etcd peer communication.

You can configure alternate ports by modifying the
[configuration file](#configuration) after installing the `pgedge-control-plane`
RPM.

### Packages

The Control Plane depends on the pgEdge Enterprise Postgres packages. The
Control Plane does not yet install Postgres or its supporting packages
automatically; install the packages on each host before starting the Control
Plane.

Run the following commands on each host:

```sh
# Install prerequisites for the pgEdge Enterprise Postgres packages
sudo dnf install -y epel-release dnf
sudo dnf config-manager --set-enabled crb

# Install the pgEdge Enterprise Postgres repository
sudo dnf install -y https://dnf.pgedge.com/reporpm/pgedge-release-latest.noarch.rpm

# Install the required packages for your Postgres version. We currently support
# versions 16, 17, and 18. Set POSTGRES_MAJOR_VERSION to your desired version.
POSTGRES_MAJOR_VERSION='<16|17|18>'
sudo dnf install -y \
      pgedge-postgresql${POSTGRES_MAJOR_VERSION} \
      pgedge-spock50_${POSTGRES_MAJOR_VERSION} \
      pgedge-postgresql${POSTGRES_MAJOR_VERSION}-contrib \
      pgedge-pgbackrest \
      pgedge-patroni
```

## Installing the RPM

The pgEdge Control Plane RPM is published with each release on the [GitHub
releases page](https://github.com/pgedge/control-plane/releases) for both
`amd64` and `arm64` architectures.

Use the following commands to download and install the RPM:

```sh
# Detect architecture
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Set the version to install
VERSION="v0.8.1"

# Download the RPM
curl -LO "https://github.com/pgedge/control-plane/releases/download/${VERSION}/pgedge-control-plane_${VERSION#v}_linux_${ARCH}.rpm"

# Install the RPM
sudo rpm -i pgedge-control-plane_${VERSION#v}_linux_${ARCH}.rpm
```

The RPM installs the following files:

- The Control Plane binary is installed at `/usr/sbin/pgedge-control-plane`.
- The systemd service unit is installed at `/usr/lib/systemd/system/pgedge-control-plane.service`.
- The default configuration file is installed at `/etc/pgedge-control-plane/config.json`.

## Configuration

The Control Plane reads its configuration from a JSON file at
`/etc/pgedge-control-plane/config.json`. The following example shows the
default configuration:

```json
{
  "orchestrator": "systemd",
  "data_dir": "/var/lib/pgedge-control-plane"
}
```

The `orchestrator` field must be set to `"systemd"` for this installation
method. The `data_dir` field specifies where the Control Plane stores its
state, including the embedded Etcd data.

The host ID defaults to the machine's short hostname. Add the `host_id` field
to set an explicit host ID:

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

The Control Plane runs as a systemd service. Use the following command to start
and enable the service:

```sh
sudo systemctl enable --now pgedge-control-plane.service
```

Use the following command to check the service status:

```sh
sudo systemctl status pgedge-control-plane.service
```

Use the following command to follow the service logs:

```sh
sudo journalctl -u pgedge-control-plane.service --follow
```

## Initializing the Control Plane

Once the service is running on all hosts, initialize and join each host the
same way as a Docker Swarm installation.

Use the following command to initialize the cluster on the first host:

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

Submit a `POST` request to each additional host's `/v1/cluster/join` endpoint
with the token and server URL from the previous step:

```sh
curl -i -X POST http://<host_ip>:3000/v1/cluster/join \
    -H 'Content-Type: application/json' \
    --data '{
        "token": "PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
        "server_urls": ["http://198.19.249.2:3000"]
    }'
```

Submit the join command to each remaining host; once all hosts have joined, you
can interact with the API from any host in the cluster. For example, you could
run the following command against any host to create a database in a three-host
cluster:

```sh
curl -X POST http://<host_ip>:3000/v1/databases \
    -H 'Content-Type:application/json' \
    --data '{
        "id": "example",
        "spec": {
            "database_name": "example",
            "database_users": [
                {
                    "username": "admin",
                    "password": "password",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "port": 5432,
            "patroni_port": 8888,
            "nodes": [
                { "name": "n1", "host_ids": ["<host 1 host id>"] },
                { "name": "n2", "host_ids": ["<host 2 host id>"] },
                { "name": "n3", "host_ids": ["<host 3 host id>"] }
            ]
        }
    }'
```

Refer to the [Creating a database](../using/create-db.md) document and other
pages in the "Using Control Plane" of our docs for more complete usage
instructions.

> [!NOTE]
> By default, each host's ID is the machine's short hostname, such as you would
> get by running `hostname -s`. You can get the host IDs for all hosts in the
> cluster from the "list hosts" endpoint on any host:
> `curl http://localhost:3000/v1/hosts`.

> [!NOTE]
> Unlike with the Swarm orchestrator, `patroni_port` is a required field in
> systemd clusters. As with other port fields, you can specify `0` to
> assign a random port.

## Performing Postgres Minor Version Upgrades

Database upgrades are not yet supported via the Control Plane API, but system
administrators can perform minor Postgres version upgrades by updating the
packages on each machine. Follow these steps on each host in the cluster:

1. Upgrade Postgres and/or other components using `dnf upgrade`. For example:

    ```sh
    sudo dnf upgrade pgedge-postgresql18
    ```

2. Find the systemd unit names for your database instances by listing units that
   have the `patroni-*` prefix:

    ```sh
    sudo systemctl list-units 'patroni-*'
    ```

3. Restart each service:

    ```sh
    sudo systemctl try-restart <service name>
    ```

To minimize the risk of downtime, we recommend upgrading one host at a time,
starting with hosts running replica instances.

After completing the upgrade on all hosts, it may take up to 30 seconds for the
new versions to be reflected in the database spec in the Control Plane API.

## Updating the Control Plane

Updating the Control Plane requires stopping the service, installing the new
RPM, and restarting the service. Download the new RPM from the [GitHub releases
page](https://github.com/pgedge/control-plane/releases) and run the following
commands:

```sh
sudo systemctl stop pgedge-control-plane.service
sudo rpm -U pgedge-control-plane-<new-version>.<arch>.rpm
sudo systemctl start pgedge-control-plane.service
```

> [!NOTE]
> The RPM upgrade (`rpm -U`) preserves your existing configuration file at
> `/etc/pgedge-control-plane/config.json` because the RPM marks it as a
> non-replaceable configuration file.

## Uninstalling the Control Plane

This section describes how to fully remove the Control Plane and its data from
your hosts.

### Standard Uninstallation

Follow these steps to remove the Control Plane after deleting all databases:

1. Delete all databases via the API.

    Use the following command to list all databases and retrieve their IDs:

    ```sh
    curl http://localhost:3000/v1/databases
    ```

    Use the following command to delete each database by ID:

    ```sh
    curl -X DELETE http://localhost:3000/v1/databases/<database_id>
    ```

    Deletions are asynchronous; wait for each task to complete before deleting
    the next database.

2. Stop and disable the Control Plane service with the following command:

    ```sh
    sudo systemctl disable --now pgedge-control-plane.service
    ```

3. Use the following command to uninstall the `pgedge-control-plane` package:

    ```sh
    sudo rpm -e pgedge-control-plane
    ```

4. Remove the Control Plane data and configuration directories.

    The data directory defaults to `/var/lib/pgedge-control-plane`; use the
    path configured in `data_dir` if you specified a custom location. Use the
    following commands to remove both directories:

    ```sh
    sudo rm -rf /var/lib/pgedge-control-plane
    sudo rm -rf /etc/pgedge-control-plane
    ```

### Manually Removing Databases

If the API cannot delete a database due to errors, remove the database manually
on each host that holds an instance.

1. Stop the Patroni services for the database instances.

    The Control Plane creates a systemd service for each Patroni instance using
    the `patroni-` prefix. Use the following command to list all Patroni
    services and identify the ones to stop:

    ```sh
    sudo systemctl list-units 'patroni-*'
    ```

    Use the following command to stop and disable each service associated with
    the database:

    ```sh
    sudo systemctl disable --now patroni-<instance_id>.service
    ```

2. Delete the instance data directories.

    By default, the Control Plane stores instance data at
    `/var/lib/pgsql/<major_version>/<instance_id>`; use the path from your
    configuration file if you set a custom instance data directory. The
    following example command removes the data directory for a Postgres 17
    instance with ID `my-instance`:

    ```sh
    sudo rm -rf /var/lib/pgsql/17/my-instance
    ```

Repeat these steps on each host that has an instance of the database.
