# Installing the Control Plane with systemd

This guide covers installing the pgEdge Control Plane on Linux hosts that use
the RPM Package Manager (RPM) package format (e.g., Red Hat Enterprise Linux
(RHEL), Rocky Linux, AlmaLinux) or the Debian (deb) package format (e.g.,
Ubuntu, Debian).

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

We'd love your feedback - please share your experience in our [GitHub issues](https://github.com/pgedge/control-plane/issues)
or join our [Discord](https://discord.com/invite/pgedge/login).

## Installing the Control Plane

After creating hosts that [meet the prerequisites](../prerequisites/index.md),
you are ready to install the Control Plane and its dependencies on each host.

The Control Plane needs Postgres, pgBackRest, and Patroni on each host. You can
install the Control Plane and these dependencies from the pgEdge Enterprise
Postgres package repositories, or you can install the dependencies from the
community Postgres package repositories and download the Control Plane package
from our [GitHub releases](https://github.com/pgEdge/control-plane/releases).
The Control Plane packages are published for both `amd64` and `arm64`
architectures.

!!! note

    The Spock package is optional. If Spock is not installed for a Postgres
    version, the Control Plane can still create databases with that version,
    but those databases are limited to a single node because Spock is required
    for replication between nodes. These single-node databases still support
    read replicas, which are created by adding more hosts to the node's
    `host_ids`. Spock is only available from the pgEdge Enterprise Postgres
    package repositories, so databases that use the community packages are
    always limited to a single node.

### pgEdge Enterprise Packages

#### RPM Packages

Follow the instructions from the ["Configuring the Repository" page](https://docs.pgedge.com/enterprise/el/configure-repo/)
of the pgEdge Enterprise Postgres package repository documentation to install
the repository.

Then, use the following commands to install the Control Plane and its
dependencies:

```sh
# Install the required packages for your Postgres version. We currently support
# versions 16, 17, and 18. Set POSTGRES_MAJOR_VERSION to your desired version.
POSTGRES_MAJOR_VERSION='<16|17|18>'
sudo dnf install -y \
      pgedge-postgresql${POSTGRES_MAJOR_VERSION} \
      pgedge-spock50_${POSTGRES_MAJOR_VERSION} \
      pgedge-postgresql${POSTGRES_MAJOR_VERSION}-contrib \
      pgedge-pgbackrest \
      pgedge-patroni \
      pgedge-control-plane
```

The `pgedge-control-plane` package will install the following files:

- `/etc/pgedge-control-plane/config.json`
- `/usr/lib/systemd/system/pgedge-control-plane.service`
- `/usr/sbin/pgedge-control-plane`
- `/usr/share/doc/pgedge-control-plane/README.md`
- `/usr/share/licenses/pgedge-control-plane/LICENSE.md`
- `/usr/share/pgedge-control-plane/pgedge-control-plane-sbom.json`
- `/usr/share/pgedge-control-plane/pgedge-control-plane-sbom.json.asc`

#### Deb Packages

Follow the instructions from the ["Configuring the Repository" page](https://docs.pgedge.com/enterprise/debian/configure-repo/)
of the pgEdge Enterprise Postgres package repository documentation to install
the repository.

Then, use the following commands to install the Control Plane and its
dependencies:

```sh
# Update the local list of available packages
sudo apt update

# Install the required packages for your Postgres version. We currently support
# versions 16, 17, and 18. Set POSTGRES_MAJOR_VERSION to your desired version.
POSTGRES_MAJOR_VERSION='<16|17|18>'
sudo apt install -y \
      pgedge-postgresql-${POSTGRES_MAJOR_VERSION} \
      pgedge-postgresql-${POSTGRES_MAJOR_VERSION}-spock50 \
      pgedge-pgbackrest \
      pgedge-patroni \
      pgedge-control-plane

# The postgresql package will create and start a default database. We recommend
# stopping and disabling that database to avoid confusion or port conflicts.
sudo systemctl disable --now postgresql.service
sudo systemctl disable --now postgresql@${POSTGRES_MAJOR_VERSION}-main.service
```

The `pgedge-control-plane` package will install the following files:

- `/etc/pgedge-control-plane/config.json`
- `/usr/lib/systemd/system/pgedge-control-plane.service`
- `/usr/sbin/pgedge-control-plane`
- `/usr/share/doc/pgedge-control-plane/README.md`
- `/usr/share/doc/pgedge-control-plane/copyright`
- `/usr/share/pgedge-control-plane/pgedge-control-plane-sbom.json`
- `/usr/share/pgedge-control-plane/pgedge-control-plane-sbom.json.asc`

### Community Packages

The community Postgres package repositories, maintained by the PostgreSQL
Global Development Group (PGDG), provide packages for Postgres, pgBackRest, and
Patroni. When you use these packages, download the `pgedge-control-plane`
package from the assets of a release on our [GitHub releases
page](https://github.com/pgEdge/control-plane/releases).

#### RPM Packages

Follow the instructions from the ["Red Hat family Linux" download page](https://www.postgresql.org/download/linux/redhat/)
of the PostgreSQL documentation to install the community repository. The
Patroni and pgBackRest packages depend on packages from the Extra Packages for
Enterprise Linux (EPEL) repository, so follow the [EPEL
documentation](https://docs.fedoraproject.org/en-US/epel/getting-started/) to
install it if you have not already.

Then, use the following commands to install the Control Plane and its
dependencies:

```sh
# Install the required packages for your Postgres version. We currently support
# versions 16, 17, and 18. Set POSTGRES_MAJOR_VERSION to your desired version.
POSTGRES_MAJOR_VERSION='<16|17|18>'
sudo dnf install -y \
      postgresql${POSTGRES_MAJOR_VERSION}-server \
      postgresql${POSTGRES_MAJOR_VERSION}-contrib \
      pgbackrest \
      patroni \
      patroni-etcd

# The postgresql-server package will create and start a default database. We
# recommend stopping and disabling that database to avoid confusion or port
# conflicts.
sudo systemctl disable --now postgresql-${POSTGRES_MAJOR_VERSION}.service

# Download the Control Plane package for your Enterprise Linux major version and
# architecture. Set CONTROL_PLANE_VERSION to the release you want to install,
# without the leading 'v', e.g. '0.11.0'.
CONTROL_PLANE_VERSION='<version>'
EL_MAJOR_VERSION="$(rpm -E '%{rhel}')"
ARCH="$(uname -m)"
CONTROL_PLANE_RPM="pgedge-control-plane-${CONTROL_PLANE_VERSION}-1.el${EL_MAJOR_VERSION}.${ARCH}.rpm"
curl -fLO "https://github.com/pgEdge/control-plane/releases/download/v${CONTROL_PLANE_VERSION}/${CONTROL_PLANE_RPM}"

# Install the downloaded package
sudo dnf install -y "./${CONTROL_PLANE_RPM}"
```

The `pgedge-control-plane` package will install the same files listed in the
[pgEdge Enterprise RPM Packages](#rpm-packages) section.

#### Deb Packages

Follow the instructions from the ["Debian" download page](https://www.postgresql.org/download/linux/debian/)
or the ["Ubuntu" download page](https://www.postgresql.org/download/linux/ubuntu/)
of the PostgreSQL documentation to install the community repository.

Then, use the following commands to install the Control Plane and its
dependencies:

```sh
# Update the local list of available packages
sudo apt update

# Install the required packages for your Postgres version. We currently support
# versions 16, 17, and 18. Set POSTGRES_MAJOR_VERSION to your desired version.
POSTGRES_MAJOR_VERSION='<16|17|18>'
sudo apt install -y \
      postgresql-${POSTGRES_MAJOR_VERSION} \
      pgbackrest \
      patroni \
      python3-etcd

# The postgresql package will create and start a default database. We recommend
# stopping and disabling that database to avoid confusion or port conflicts.
sudo systemctl disable --now postgresql.service
sudo systemctl disable --now postgresql@${POSTGRES_MAJOR_VERSION}-main.service

# Download the Control Plane package for your distribution and architecture. Set
# CONTROL_PLANE_VERSION to the release you want to install, without the leading
# 'v', e.g. '0.11.0'.
CONTROL_PLANE_VERSION='<version>'
CODENAME="$(. /etc/os-release && echo "${VERSION_CODENAME}")"
ARCH="$(dpkg --print-architecture)"
CONTROL_PLANE_DEB="pgedge-control-plane_${CONTROL_PLANE_VERSION}-1.${CODENAME}_${ARCH}.deb"
curl -fLO "https://github.com/pgEdge/control-plane/releases/download/v${CONTROL_PLANE_VERSION}/${CONTROL_PLANE_DEB}"

# Install the downloaded package
sudo apt install -y "./${CONTROL_PLANE_DEB}"
```

The `pgedge-control-plane` package will install the same files listed in the
[pgEdge Enterprise Deb Packages](#deb-packages) section.

!!! note

    Packages installed from GitHub releases are not updated by `dnf upgrade` or
    `apt upgrade`. To upgrade the Control Plane, repeat the download and install
    steps with the new version.

## Configuration

The Control Plane reads its configuration from
`/etc/pgedge-control-plane/config.json`. The following example shows the default
configuration:

```json
{
  "orchestrator": "systemd",
  "data_dir": "/var/lib/pgedge-control-plane"
}
```

The `orchestrator` field must be set to `"systemd"` for this installation
method. The `data_dir` field specifies where the Control Plane stores its state,
including the embedded Etcd data.

By default, the Control Plane follows the operating system's conventions for
Postgres data directories. For RHEL-like distributions, the base directory
defaults to `/var/lib/pgsql`. For Debian-based distributions, the default base
directory is `/var/lib/postgresql`. Add the `systemd.instance_data_dir` to the
configuration file to set a custom base directory:

```json
{
  "orchestrator": "systemd",
  "data_dir": "/data/pgedge-control-plane",
  "systemd": {
    "instance_data_dir": "/data/postgres"
  }
}
```

The host ID defaults to the machine's short hostname. Add the `host_id` field to
set an explicit host ID:

```json
{
  "orchestrator": "systemd",
  "data_dir": "/var/lib/pgedge-control-plane",
  "host_id": "my-host-1"
}
```

You can find the full list of configuration settings in the [Configuration
reference](./configuration.md).

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

Once the service is running on all hosts, initialize and join each host as in a
Docker Swarm installation.

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
> This example requires Spock because it creates a database with three nodes.
> If you installed the community packages, specify a single node instead.

> [!NOTE]
> Unlike with the Swarm orchestrator, `patroni_port` is a required field in
> systemd clusters. As with other port fields, you can specify `0` to assign a
> random port.

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

2. Use one of the following commands to uninstall the `pgedge-control-plane`
   package:

    ```sh
    # If you installed the RPM package, run:
    sudo rpm -e pgedge-control-plane

    # If you installed the deb package, run:
    sudo apt remove pgedge-control-plane
    ```

3. Remove the Control Plane data and configuration directories.

    The data directory defaults to `/var/lib/pgedge-control-plane`; use the path
    configured in `data_dir` if you specified a custom location. Use the
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

    By default, the Control Plane follows the OS conventions for Postgres data
    directories. On RHEL-like distributions, this defaults to
    `/var/lib/pgsql/<major_version>/<instance_id>`. On Debian-based
    distributions, this defaults to `/var/lib/postgresql/<major version>/<instance id>`. 
    Use the path from your configuration file if you set a custom instance data
    directory. The following example command removes the data directory for a
    Postgres 17 instance with ID `my-instance` on an RHEL-like distribution:

    ```sh
    sudo rm -rf /var/lib/pgsql/17/my-instance
    ```

Repeat these steps on each host that has a database instance.
