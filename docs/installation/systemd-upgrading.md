# Upgrading the Control Plane with systemd
Updating the Control Plane just involves installing the new package. This will
automatically restart the Control Plane service after the update is complete.

> [!NOTE]
> The package upgrade will preserve any modifications to the configuration file
> at `/etc/pgedge-control-plane/config.json`.

We recommend updating the Control Plane using the pgEdge Enterprise package
repositories you configured in the [Packages](systemd-installation.md#packages)
section. If you installed the Control Plane manually, see [Updating from
GitHub Releases](#updating-from-github-releases) below.

## Updating the Control Plane

### RPM Package

```sh
# (Optional) print the current version via the API
curl http://localhost:3000/v1/version

# Upgrade the package
sudo dnf upgrade -y pgedge-control-plane

# (Optional) print the updated version via the API
curl http://localhost:3000/v1/version
```

### Deb Package

```sh
# (Optional) print the current version via the API
curl http://localhost:3000/v1/version

# Upgrade the package
sudo apt install --only-upgrade -y pgedge-control-plane

# (Optional) print the updated version via the API
curl http://localhost:3000/v1/version
```

### Updating from GitHub Releases

If you installed the Control Plane manually from GitHub releases, download and
install the updated package version.

#### RPM Package

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

#### Deb Package

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
