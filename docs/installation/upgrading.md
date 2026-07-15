# Upgrading the Control Plane (Docker Swarm)
We publish a new Docker image whenever we release a new version of the Control
Plane. The Control Plane version is specified in the image property of the [stack definition file](../installation/swarm-installation.md#creating-the-stack-definition-file):

```yaml
services:
  host-1:
    image: ghcr.io/pgedge/control-plane:v0.9.0
```

You can *pin* to a specific version by including a version in the `image`
fields in your service specification, such as `ghcr.io/pgedge/control-plane:v0.9.0`. 

If you do not include a version, Docker will pull the `ghcr.io/pgedge/control-plane:latest` tag by default. 

!!! note

    We recommend pinning the version in production environments so that upgrades are explicit and predictable.

## Upgrading with a Pinned Version

To upgrade from a pinned version:

1. Modify the `image` fields in your service specification to reference the new version, such as updating `ghcr.io/pgedge/control-plane:v0.4.0` to `ghcr.io/pgedge/control-plane:v0.5.0`.
   
2. Re-run `docker stack deploy -c control-plane.yaml control-plane` as in the [Deploying the stack](swarm-installation.md#deploying-the-stack) section.

## Upgrading with the `latest` Tag

By default, `docker stack deploy` will always query the registry for updates
unless you've specified a different `--resolve-image` option; updating with
the `latest` tag is a single step:

1. Re-run `docker stack deploy -c control-plane.yaml control-plane` as described in the
   [Deploying the stack](swarm-installation.md#deploying-the-stack) section.

## How to Check the Current Version

If you're not sure which version you're running, such as when you're using the
`latest` tag, you can check the version of a particular Control Plane server
with the `/v1/version` API endpoint:

=== "curl"

    ```sh
    curl http://host_ip_address:3000/v1/version
    ```

Where `host_ip_address` specifies the IP address of the node.  For example:

```sh
curl http://10.177.149.2:3000/v1/version
{"version":"v0.5.1-0.20251119153303-d1f3c883fa41","revision":"d1f3c883fa415db1cc62ab329100d9579cdb5d68","revision_time":"2025-11-19T15:33:03Z","arch":"arm64"}
```

# Upgrading the Control Plane (systemd)
Updating the Control Plane just involves installing the new package. This will
automatically restart the Control Plane service after the update is complete.

> [!NOTE]
> The package upgrade will preserve any modifications to the configuration file
> at `/etc/pgedge-control-plane/config.json`.

### RPM Package

Use the following commands to download and install the updated RPM:

```sh
# (Optional) print the current version via the API
curl http://localhost:3000/v1/version

# Detect architecture
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Set the new version to install
VERSION="v0.9.0"

# Download the RPM
curl -LO "https://github.com/pgedge/control-plane/releases/download/${VERSION}/pgedge-control-plane_${VERSION#v}_linux_${ARCH}.rpm"

# Install the RPM with the 'upgrade' flag
sudo rpm -U pgedge-control-plane_${VERSION#v}_linux_${ARCH}.rpm

# (Optional) print the updated version via the API
curl http://localhost:3000/v1/version
```


### Deb Package

Use the following commands to download and install the deb package:

```sh
# (Optional) print the current version via the API
curl http://localhost:3000/v1/version

# Detect architecture
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Set the new version to install
VERSION="v0.9.0"

# Download the deb package
curl -LO --output-dir /tmp "https://github.com/pgedge/control-plane/releases/download/${VERSION}/pgedge-control-plane_${VERSION#v}_linux_${ARCH}.deb"

# Install the deb package
sudo apt install /tmp/pgedge-control-plane_${VERSION#v}_linux_${ARCH}.deb

# (Optional) print the updated version via the API
curl http://localhost:3000/v1/version
```

## Performing Postgres Minor Version Upgrades

Database upgrades are not yet supported via the Control Plane API, but system
administrators can perform minor Postgres version upgrades by updating the
packages on each machine. Follow these steps on each host in the cluster:

1. Upgrade Postgres and/or other components using `dnf upgrade` or
   `apt install --only-upgrade`. For example, to upgrade Postgres 18:

    ```sh
    # If your system uses dnf, run:
    sudo dnf upgrade pgedge-postgresql18

    # If your system uses apt, run:
    sudo apt install --only-upgrade pgedge-postgresql-18
    ```

1. Find the systemd unit names for your database instances by listing units that
   have the `patroni-*` prefix:

    ```sh
    sudo systemctl list-units 'patroni-*'
    ```

2. Restart each service:

    ```sh
    sudo systemctl try-restart <service name>
    ```

To minimize the risk of downtime, we recommend upgrading one host at a time,
starting with hosts running replica instances.

After completing the upgrade on all hosts, it may take up to 30 seconds for the
new versions to be reflected in the database spec in the Control Plane API.
