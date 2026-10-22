# Upgrading the Control Plane with systemd
Updating the Control Plane just involves installing the new package. This will
automatically restart the Control Plane service after the update is complete.

> [!NOTE]
> The package upgrade will preserve any modifications to the configuration file
> at `/etc/pgedge-control-plane/config.json`.

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

2. Upgrade the Spock package the same way. For example, to upgrade the
   Spock 5 package on Postgres 18:

    ```sh
    # If your system uses dnf, run:
    sudo dnf upgrade pgedge-spock50_18

    # If your system uses apt, run:
    sudo apt install --only-upgrade pgedge-postgresql-18-spock50
    ```

> [!NOTE]
> Restarting Postgres after installing a new Spock package triggers
> Spock's background worker to detect the version mismatch and migrate
> the extension schema automatically; no manual `ALTER EXTENSION`
> step is required. For a production cluster, disable
> `spock.enable_ddl_replication` before upgrading and re-enable it only
> after every node is on the new version. See
> [Upgrading the Spock Extension](https://docs.pgedge.com/spock-v5/upgrading_spock/)
> for the full procedure, including replication verification steps.

3. Find the systemd unit names for your database instances by listing units that
   have the `patroni-*` prefix:

    ```sh
    sudo systemctl list-units 'patroni-*'
    ```

4. Restart each service:

    ```sh
    sudo systemctl try-restart <service name>
    ```

To minimize the risk of downtime, we recommend upgrading one host at a time,
starting with hosts running replica instances.

After completing the upgrade on all hosts, it may take up to 30 seconds for the
new versions to be reflected in the database spec in the Control Plane API.

### Applying output_plugin_libraries After the Upgrade

Postgres 16.15, 17.11, 18.6, and later minor versions gate logical
decoding output plugins behind the `output_plugin_libraries`
allow-list. See
[output_plugin_libraries and Postgres Minor Versions](../using/create-db.md#output_plugin_libraries-and-postgres-minor-versions)
for why Spock needs this setting once a node is on a patched minor
version.

A manual package upgrade and service restart does not push
`output_plugin_libraries` to Patroni. The Control Plane reflects the
new Postgres version in the database spec within about 30 seconds of
the restart, but the GUC itself is only applied to Patroni as part of
an API-driven spec update, which this manual upgrade flow does not
trigger. You must submit that spec update yourself to restore Spock
replication after a manual minor version upgrade.

> [!WARNING]
> Confirm that every targeted host is running a patched minor version
> (16.15, 17.11, 18.6, or later) before you set
> `output_plugin_libraries`. Postgres refuses to start with
> `unrecognized configuration parameter "output_plugin_libraries"` if
> the setting is present on an older, unpatched minor version.

Once the target host or hosts are confirmed on a patched minor
version, submit a spec update that explicitly sets
`postgresql_conf.output_plugin_libraries` to the full list of output
plugins Spock and any other logical decoding consumers need:

```sh
curl -X POST http://localhost:3000/v1/databases/example \
    -H 'Content-Type:application/json' \
    --data '{
        "id": "example",
        "spec": {
            "database_name": "example",
            "postgresql_conf": {
                "output_plugin_libraries": "pgoutput, test_decoding, spock_output"
            },
            "nodes": [
                { "name": "n1", "host_ids": ["host-1"] },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

> [!NOTE]
> `database_name` and `nodes` are required on every spec update, and
> `nodes` must list every node currently in the database; omitting a
> node removes it. Other optional fields, such as `database_users`,
> are preserved from the existing spec if you leave them out. See
> [Updating a Database](../using/update-db.md) for more detail on the
> update request format.

If you upgrade hosts one at a time rather than all at once, apply the
override per node with `nodes[].postgresql_conf` instead of setting it
database-wide, until every node in the database is confirmed on a
patched minor version:

```sh
curl -X POST http://localhost:3000/v1/databases/example \
    -H 'Content-Type:application/json' \
    --data '{
        "id": "example",
        "spec": {
            "database_name": "example",
            "nodes": [
                {
                    "name": "n1",
                    "host_ids": ["host-1"],
                    "postgresql_conf": {
                        "output_plugin_libraries": "pgoutput, test_decoding, spock_output"
                    }
                },
                { "name": "n2", "host_ids": ["host-2"] },
                { "name": "n3", "host_ids": ["host-3"] }
            ]
        }
    }'
```

> [!NOTE]
> Include every node in the `nodes` array even though only `n1` gets
> the override; a `nodes` entry that is left out of the request is
> removed from the database.
