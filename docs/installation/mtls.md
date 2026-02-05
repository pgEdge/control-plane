# Configuring mTLS

The Control Plane supports Mutual Transport Layer Security (mTLS) to encrypt and authenticate client and inter-server communications with user-supplied certificates. In this configuration, the Control Plane serves HTTPS rather than HTTP.

## Enabling mTLS on the Control Plane Servers

Each Control Plane server is both an HTTP(S) server and an HTTP(S) client of other Control Plane servers in the cluster. This means that each server needs both a server certificate and a client certificate.

!!! important

    With mTLS enabled, clients will verify that the server they're connecting to (by IP address or DNS name) is listed in the Common Name (CN) or Subject Alternative Name (SAN) of the server's certificate. It's important that the server's certificate include every address and DNS name that you will use to connect to it. Note that, by default, inter-server communication uses the IPv4 address configured for the server (see [configuration.md](./configuration.md). If no address is configured, the server will automatically detect the host machine's IP address. You can see which IP address it will automatically detect by running `ip route get 1` on the host machine.

You'll need to place the following files in a directory on the host machine for each Control Plane server:

- The Certificate Authority (CA) certificate
- A server certificate
- A server key
- A client certificate
- A client key

!!! tip

    If your organization allows it, you can use the same certificate for both the client and server certificates, provided that the certificate includes both 'Server Authentication' and 'Client Authentication' in the Extended Key Usage (EKU). We demonstrate this in our tutorial.

Then you can use the certificates by adding the directory to the `volumes` section for each `service` in the [Control Plane stack definition file](./installation.md#creating-the-stack-definition-file) and setting these variables in the `environment` sections:

- `PGEDGE_HTTP__CA_CERT`: the path to the CA certificate
- `PGEDGE_HTTP__SERVER_CERT`: the path to the server certificate
- `PGEDGE_HTTP__SERVER_KEY`: the path to the server key
- `PGEDGE_HTTP__CLIENT_CERT`: the path to the client certificate
- `PGEDGE_HTTP__CLIENT_KEY`: the path to the client key

For example, if you've placed the certificates in a `/opt/pgedge/control-plane` on each host machine your stack definition might look like:

```yaml
services:
  host-1:
    image: ghcr.io/pgedge/control-plane:v0.6.2
    command: run
    environment:
      - PGEDGE_HOST_ID=host-1
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
      - PGEDGE_HTTP__CA_CERT=/opt/pgedge/control-plane/ca.crt
      - PGEDGE_HTTP__SERVER_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__SERVER_KEY=/opt/pgedge/control-plane/server.key
      - PGEDGE_HTTP__CLIENT_CERT=/opt/pgedge/control-plane/client.crt
      - PGEDGE_HTTP__CLIENT_KEY=/opt/pgedge/control-plane/client.key
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/pgedge/control-plane:/opt/pgedge/control-plane
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==vzou89zyd4n3xz6p6jvoohqxx
  host-2:
    # Some lines omitted for brevity
    environment:
      # ...
      - PGEDGE_HTTP__CA_CERT=/opt/pgedge/control-plane/ca.crt
      - PGEDGE_HTTP__SERVER_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__SERVER_KEY=/opt/pgedge/control-plane/server.key
      - PGEDGE_HTTP__CLIENT_CERT=/opt/pgedge/control-plane/client.crt
      - PGEDGE_HTTP__CLIENT_KEY=/opt/pgedge/control-plane/client.key
    volumes:
      # ...
      - /opt/pgedge/control-plane:/opt/pgedge/control-plane
    # ...
  host-3:
    # ...
    environment:
      # ...
      - PGEDGE_HTTP__CA_CERT=/opt/pgedge/control-plane/ca.crt
      - PGEDGE_HTTP__SERVER_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__SERVER_KEY=/opt/pgedge/control-plane/server.key
      - PGEDGE_HTTP__CLIENT_CERT=/opt/pgedge/control-plane/client.crt
      - PGEDGE_HTTP__CLIENT_KEY=/opt/pgedge/control-plane/client.key
    volumes:
      # ...
      - /opt/pgedge/control-plane:/opt/pgedge/control-plane
    # ...
```

## Enabling mTLS on Clients

Clients need the following files to communicate with a Control Plane server that has mTLS enabled:

- The Certificate Authority (CA) certificate
- A client certificate
- A client key

How the files are provided to the client will depend on the application. With cURL, for example, you would use the `--cacert`, `--cert`, and `--key` options to provide the path of each file:

```sh
curl 'https://host-1:3000/v1/version' \
    --cacert ./ca.crt \
    --cert ./client.crt \
    --key ./client.key
```

## Tutorial

This tutorial demonstrates the process of generating and using certificates for mTLS with the Control Plane. For the purpose of the tutorial, we'll assume three host machines with these attributes:

- Hostname: `host-1`
  IP Address: `192.168.105.2`
- Hostname: `host-2`
  IP Address: `192.168.105.3`
- Hostname: `host-3`
  IP Address: `192.168.105.4`

### Generating Certificates

In a production setup, we would use our organization's Public Key Infrastructure (PKI) to create and rotate certificates. However, to keep this tutorial self-contained, we're going to use the [`step` CLI](https://github.com/smallstep/cli) with a local certificate authority. See the [`step` CLI installation page](https://smallstep.com/docs/step-cli/installation/) for installation instructions.

```sh
# Generate a root certificate authority. This command will prompt for a password
# to encrypt the CA key.
step certificate create tutorial ./ca.crt ./ca.key \
  --profile=root-ca \
  --not-after 8760h

# Generate certificates for each server. These commands will prompt for the CA
# key, but each new key is passwordless. Note that we're using the hostname as
# the Common Name (CN) and including the IP address as a Subject Alternative
# Name (SAN). The step CLI's 'leaf' profile includes both server and client
# authentication in the Extended Key Usage (EKU).
step certificate create host-1 host-1.crt host-1.key \
  --san '192.168.105.2' \
  --no-password \
  --insecure \
  --profile leaf \
  --not-after 2160h \
  --ca ./ca.crt \
  --ca-key ./ca.key

step certificate create host-2 host-2.crt host-2.key \
  --san '192.168.105.3' \
  --no-password \
  --insecure \
  --profile leaf \
  --not-after 2160h \
  --ca ./ca.crt \
  --ca-key ./ca.key

step certificate create host-3 host-3.crt host-3.key \
  --san '192.168.105.4' \
  --no-password \
  --insecure \
  --profile leaf \
  --not-after 2160h \
  --ca ./ca.crt \
  --ca-key ./ca.key

# Generate the client certificate. We're also using a passwordless key here for
# simplicity, but this will depend on your use case.
step certificate create client client.crt client.key \
  --no-password \
  --insecure \
  --profile leaf \
  --not-after 2160h \
  --ca ./ca.crt \
  --ca-key ./ca.key
```

Now, we can distribute our certificates to each Control Plane host machine:

```sh
# Make new directories for our certificates
ssh host-1 'sudo mkdir -p /opt/pgedge/control-plane'
ssh host-2 'sudo mkdir -p /opt/pgedge/control-plane'
ssh host-3 'sudo mkdir -p /opt/pgedge/control-plane'

# Copy the server certificates and keys to each machine. We're reading them from
# stdin rather than using scp to keep the tutorial short.
ssh host-1 'sudo tee /opt/pgedge/control-plane/ca.crt > /dev/null' < ./ca.crt
ssh host-1 'sudo tee /opt/pgedge/control-plane/server.crt > /dev/null' < ./host-1.crt
ssh host-1 'sudo tee /opt/pgedge/control-plane/server.key > /dev/null' < ./host-1.key

ssh host-2 'sudo tee /opt/pgedge/control-plane/ca.crt > /dev/null' < ./ca.crt
ssh host-2 'sudo tee /opt/pgedge/control-plane/server.crt > /dev/null' < ./host-2.crt
ssh host-2 'sudo tee /opt/pgedge/control-plane/server.key > /dev/null' < ./host-2.key

ssh host-3 'sudo tee /opt/pgedge/control-plane/ca.crt > /dev/null' < ./ca.crt
ssh host-3 'sudo tee /opt/pgedge/control-plane/server.crt > /dev/null' < ./host-3.crt
ssh host-3 'sudo tee /opt/pgedge/control-plane/server.key > /dev/null' < ./host-3.key
```

### Configuring the Control Plane

Now that our certificates are available on each machine, we can update our stack YAML to use them. Note that the node IDs and directories may be different in your installation:

```yaml
services:
  host-1:
    image: ghcr.io/pgedge/control-plane:v0.6.2
    command: run
    environment:
      - PGEDGE_HOST_ID=host-1
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
      - PGEDGE_HTTP__CA_CERT=/opt/pgedge/control-plane/ca.crt
      - PGEDGE_HTTP__SERVER_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__SERVER_KEY=/opt/pgedge/control-plane/server.key
      # We're able to use the same certificate as the server's client
      # certificate because it includes 'Client Authentication' in its Extended
      # Key Usage.
      - PGEDGE_HTTP__CLIENT_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__CLIENT_KEY=/opt/pgedge/control-plane/server.key
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/pgedge/control-plane:/opt/pgedge/control-plane
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==81kw1zwmh9y8hk4rd7igylry0
  host-2:
    image: ghcr.io/pgedge/control-plane:v0.6.2
    command: run
    environment:
      - PGEDGE_HOST_ID=host-2
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
      - PGEDGE_HTTP__CA_CERT=/opt/pgedge/control-plane/ca.crt
      - PGEDGE_HTTP__SERVER_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__SERVER_KEY=/opt/pgedge/control-plane/server.key
      - PGEDGE_HTTP__CLIENT_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__CLIENT_KEY=/opt/pgedge/control-plane/server.key
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/pgedge/control-plane:/opt/pgedge/control-plane
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==xz7069ytbdq7uzd2tvrj2wlf2
  host-3:
    image: ghcr.io/pgedge/control-plane:v0.6.2
    command: run
    environment:
      - PGEDGE_HOST_ID=host-3
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
      - PGEDGE_HTTP__CA_CERT=/opt/pgedge/control-plane/ca.crt
      - PGEDGE_HTTP__SERVER_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__SERVER_KEY=/opt/pgedge/control-plane/server.key
      # We're able to use the same certificate as the server's client
      # certificate because it includes 'Client Authentication' in its Extended
      # Key Usage.
      - PGEDGE_HTTP__CLIENT_CERT=/opt/pgedge/control-plane/server.crt
      - PGEDGE_HTTP__CLIENT_KEY=/opt/pgedge/control-plane/server.key
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/pgedge/control-plane:/opt/pgedge/control-plane
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==gf3ny9opk0idbq0t2c7lrxpks
networks:
  host:
    name: host
    external: true
```

Now, we're able to deploy our stack YAML from any of the Docker Swarm manager hosts:

```sh
docker stack deploy -c control-plane.yaml control-plane
```

### Connecting With mTLS

Once the deploy is complete, we'll need to use our client certificate to access the Control Plane API. With the cURL command line, for example, this looks like:

```sh
# Note that we need to use https now
curl https://host-1:3000/v1/version \
  --cacert ./ca.crt \
  --cert ./client.crt \
  --key ./client.key
```
