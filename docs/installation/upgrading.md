# Upgrading the Control Plane

We publish a new Docker image whenever we release a new version of the Control
Plane. The Control Plane version is specified in the image property of the [stack definition file](../installation/installation.md#creating-the-stack-definition-file):

```yaml
services:
  host-1:
    image: ghcr.io/pgedge/control-plane:<< control_plane_version >>
```

You can *pin* to a specific version by including a version in the `image`
fields in your service specification, such as `ghcr.io/pgedge/control-plane:<< control_plane_version >>`. 

If you do not include a version, Docker will pull the `ghcr.io/pgedge/control-plane:latest` tag by default. 

!!! note

    We recommend pinning the version in production environments so that upgrades are explicit and predictable.

## Upgrading with a Pinned Version

To upgrade from a pinned version:

1. Modify the `image` fields in your service specification to reference the new version, such as updating `ghcr.io/pgedge/control-plane:v0.4.0` to `ghcr.io/pgedge/control-plane:v0.5.0`.
   
2. Re-run `docker stack deploy -c control-plane.yaml control-plane` as in the [Deploying the stack](installation.md#deploying-the-stack) section.

## Upgrading with the `latest` Tag

By default, `docker stack deploy` will always query the registry for updates
unless you've specified a different `--resolve-image` option; updating with
the `latest` tag is a single step:

1. Re-run `docker stack deploy -c control-plane.yaml control-plane` as described in the
   [Deploying the stack](installation.md#deploying-the-stack) section.

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
