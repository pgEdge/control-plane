# Running the Control Plane locally

The `docker/control-plane-dev` directory contains configuration for a six-host
Control Plane cluster that runs in Docker via Docker Compose.

## Prerequisites

Before deploying the Control Plane in a development environment, you must install:

* Docker Desktop - for details, visit the [official download page](https://www.docker.com/products/docker-desktop/).

* Go 1.20 + - for details, visit the [official download page](https://go.dev/doc/install)


### Configuration

After meeting prerequisites on your system, make sure to change the settings to
provide adequate [disk space, CPU, and RAM](https://docs.docker.com/desktop/settings-and-maintenance/settings/#resources). Use the following as a baseline configuration:

- 100% of available CPUs
- 50% of available RAM
- 60GB of disk space
  - You can use `docker system df` to monitor available space and increase this
    as needed.

> [!IMPORTANT]
> Our Docker Compose configuration uses host networking, so you must also enable
> [the host networking setting](https://docs.docker.com/engine/network/drivers/host/#docker-desktop).

### Restish

Restish is a CLI tool to interact with REST APIs that expose an OpenAPI spec,
like the Control Plane API. It's not strictly required, but we recommend it.

[Installation guide](https://rest.sh/#/guide)

```sh
brew install rest-sh/tap/restish
```

We recommend you add this environment variable to your `.zshrc` as well to
disable Restish's default retry behavior:

```sh
export RSH_RETRY=0
```

You should also you set alias restish="noglob restish" in your ~/.zshrc to prevent it from trying to handle `?` in URLs and `[]` in shorthand input. Alternatively you can use quotes around your inputs.

```sh
export alias restish="noglob restish"
```

The changes to your `.zshrc` will automatically apply to new sessions. To reload the configuration in your current shell session, run:

```sh
exec zsh
```

After installing Restish, use the following command to verify the installation and initialize your configuration file:

```sh
restish --help
```

On MacOS, the full path to the Restish configuration file is `~/Library/Application Support/restish/apis.json`. See [the configuration documentation](https://rest.sh/#/configuration) to find the configuration file location for non-MacOS systems. Update the configuration file to contain the following details for the Control Plane deployment:

```json
{
  "$schema": "https://rest.sh/schemas/apis.json",
  "control-plane-local-1": {
    "base": "http://localhost:3000",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-local-2": {
    "base": "http://localhost:3001",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-local-3": {
    "base": "http://localhost:3002",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-local-4": {
    "base": "http://localhost:3003",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-local-5": {
    "base": "http://localhost:3004",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-local-6": {
    "base": "http://localhost:3005",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  }
}
```

## Running the Control Plane

To start the Control Plane instances, navigate into the `control-plane` repository root and run:

```sh
make dev-watch
```

This will build a `control-plane` binary, build the Docker image in `docker/control-plane-dev`, and run the Docker Compose configuration in `watch` mode. See the [Development workflow](#development-workflow) section to learn how to use this setup for development.

## Interact with the Control Plane API

Now, you should be able to interact with the API using Restish. For example, to
initialize a new cluster and create a new database:

```sh
restish control-plane-local-1 init-cluster
restish control-plane-local-2 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-3 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-4 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-5 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-6 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-1 create-database '{
  "id": "storefront",
  "spec": {
    "database_name": "storefront",
    "database_users": [
      {
        "username": "admin",
        "password": "password",
        "db_owner": true,
        "attributes": ["SUPERUSER", "LOGIN"]
      },
      {
        "username": "app",
        "password": "password",
        "attributes": ["LOGIN"],
        "roles": ["pgedge_application"]
      }
    ],
    "nodes": [
      { "name": "n1", "host_ids": ["host-1", "host-4"] },
      { "name": "n2", "host_ids": ["host-2", "host-5"] },
      { "name": "n3", "host_ids": ["host-3", "host-6"] }
    ]
  }
}'
```

The API is under active development. You can find the current set of endpoints
in:

- the autogenerated help text in `restish`: `restish control-plane-local-1 --help`.
- the Go source of the API specification in `api/apiv1/design`.
- the generated OpenAPI spec in `api/apiv1/gen/http/openapi.yaml`.

Endpoints that are unimplemented will return a `not implemented` error.

## Resetting your Development Environment

To reset your environment to its initial state, run:

```
make dev-teardown
```

This will:

- shutdown the control plane.
- remove any databases and database networks.
- remove the data directories for each instance.

When you start the control plane again with `make dev-watch`, it will be in an
uninitialized state. Then, you can follow the instructions in the
[Interact with the Control Plane API](#interact-with-the-control-plane-api)
section to reinitialize your cluster.

## Development Workflow

The following sections detail the steps in the development process.

### Rebuilding the `control-plane` binary

The Docker Compose file is configured to watch for changes to the
`control-plane` binary. You can update the binary in the running containers by
running:

```sh
make dev-build
```

You'll see messages in the docker compose output to indicate that it's stopping
the containers, syncing the files, and then starting them up again. This takes
about 10 seconds due to the graceful shutdown in the Control Plane server.

### Debugging

The `control-plane-dev` image includes the Delve Go debugger. You can run the
debugger by adding the `DEBUG` environment variable to the `make dev-watch`
command:

```sh
DEBUG=1 make dev-watch
```

This will run the debugger in the `host-1` Control Plane container. The debugger
will wait until you've attached to it before starting the Control Plane server.
This is an example remote debugging configuration for VSCode:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Docker debug",
      "type": "go",
      "request": "attach",
      "mode": "remote",
      "remotePath": "${workspaceFolder}",
      "port": 2345,
      "host": "localhost", 
    }
  ]
}
```

After attaching the debugger, the server will start normally.

## API Documentation

The `docker-compose.yaml` file for this configuration includes an API
documentation server. You can access the documentation in your browser at
http://localhost:8999.

This uses the OpenAPI spec from the `api/apiv1/gen` directory and generates the
documentation on the client side. When you regenerate the OpenAPI spec, for
example by running `make -C api generate`, you only need to refresh the page to
see the updates.

## Optional Development Tools

The tools listed below may be helpful in your development environment.

### `dev-env.zsh` Script

If you use `zsh`, you may be interested in the `dev-env.zsh` script which adds
some helpful functions and aliases to your shell. There's also an optional Oh My
Zsh plugin as well as a theme that uses the plugin. See the `hack/dev-env.md`
file for installation and usage instructions.

### Bruno

[Bruno](https://www.usebruno.com/) is an open source API client that makes it
easy to share canned requests via Git. This repository includes a Bruno
collection called `test-scenarios` that we use to share manual test scenarios.

The `test-scenarios` collection and each of the test scenarios within have their
own documentation that you can view either in the source files or within the
Bruno client.

We recommend using the standalone Bruno API client rather than the VSCode
extension because we make extensive use of the developer console. If you're
using MacOS, you can install Bruno through HomeBrew:

```
brew install bruno
```

#### Bruno's `wait_for_task` Helper

If you're adding a new request that triggers an asynchronous operation, like
creating a database, you can use the `wait_for_task` helper by adding a
pre-request variable to your request:

```
wait_for_task: true
```

This helper will print the task logs to the Bruno client's development console
and block until the task is completed, failed, or canceled. See the requests in
the `local-backup-and-restore` scenario for examples.

#### When Should I Add to the Test Scenarios?

These scenarios are an optional tool that are meant to make it easier for you to
develop and test changes. They can also be helpful for reviewers who need to
test your changes. Consider adding new requests or scenarios if you find
yourself repeating the same sequence of requests during development, and those
requests aren't already covered by an existing scenarios.
