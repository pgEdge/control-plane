# Development environment helpers for zsh

This document describes the `dev-env.zsh` helper functions as well as the
`pgedge-cp-env` theme and plugin for Oh My Zsh.

- [Development environment helpers for zsh](#development-environment-helpers-for-zsh)
- [`dev-env.zsh`](#dev-envzsh)
  - [Prerequisites](#prerequisites)
  - [Usage](#usage)
    - [Switching environments](#switching-environments)
    - [Restish configuration](#restish-configuration)
    - [Aliases](#aliases)
    - [Utility functions](#utility-functions)
      - [`cp-psql`](#cp-psql)
      - [`cp-docker-exec`](#cp-docker-exec)
- [`pgedge-cp-env` integration for Oh My Zsh](#pgedge-cp-env-integration-for-oh-my-zsh)
  - [Prerequisites](#prerequisites-1)
  - [Installation](#installation)
  - [Usage](#usage-1)
    - [Plugin](#plugin)
    - [Theme](#theme)
      - [Switching themes dynamically](#switching-themes-dynamically)

# `dev-env.zsh`

This file contains helper functions for interacting with the control plane in
the [Docker Compose development setup](../docs/development/running-locally-docker.md)
as well as the [Lima and EC2 E2E fixtures](../docs/development/e2e-tests.md#test-fixtures).

## Prerequisites

- `jq`
- `yq`
- `coreutils`
- `sk`
- `restish`
- `etcd`

```sh
brew install \
    jq \
    yq \
    coreutils \
    sk \
    rest-sh/tap/restish \
    etcd
```

## Usage

`source` the `dev-env.zsh` file to use the helper functions:

```sh
source ./hack/dev-env.zsh
```

You can modify this line to use an absolute path and add it to your `~/.zshrc`
file to apply it in every new shell. If you do, you can run `exec zsh` in any
existing shell sessions to apply the `~/.zshrc` changes.

### Switching environments

By default, the aliases and functions added by `dev-env.zsh` will point to your
local Docker Compose-based environment. The `use-*` functions let you switch to
other environments:

```sh
# The local Docker Compose-based environment
use-compose

# The Lima test fixtures
use-lima

# The EC2 test fixtures
use-ec2
```

You can see the current environment in the `CP_ENV` environment variable:

```sh
echo "${CP_ENV}"
```

See the [oh-my-zsh integration](#pgedge-cp-env-integration-for-oh-my-zsh) below
for how to include `${CP_ENV}` in your shell prompt.

### Restish configuration

The functions in this script create separate `restish` configuration and cache
directories for each environment so that `restish host-1` always points to
`host-1` for the current environment, `restish host-2` to the environment's
`host-2`, etc. It works by modifying the `RESTISH_CONFIG_DIR` and
`RESTISH_CACHE_DIR` environment variables.

### Aliases

This script creates several dynamic aliases:

```sh
# aliases for 'restish host-*'
cp1-req
cp2-req
cp3-req
# the compose environment has three additional hosts
cp4-req
cp5-req
cp6-req

# aliases for 'restish api sync host-*'
cp1-api-sync
cp2-api-sync
cp3-api-sync
# the compose environment has three additional hosts
cp4-api-sync
cp5-api-sync
cp6-api-sync

# ssh commands for the lima and ec2 environments
cp1-ssh
cp2-ssh
cp3-ssh
```

It also creates two static aliases that only apply to the Docker Compose
environment:

```sh
# shortcut for etcdctl that connects to host-1 in the Docker Compose setup
cp-etcdctl

# shortcut for docker compose with the control-plane-dev compose configuration
cp-docker-compose
```

### Utility functions

#### `cp-psql`

This function is a shortcut for connecting to an Instance with `psql` either via
`docker exec` or via a locally-running `psql` client. It works with every
environment and handles SSH, TTY allocation, etc. The included help text for
this function demonstrates the intended use:

```
> cp-psql --help

cp-psql [-h|--help]
cp-psql <-i|--instance-id instance id> <-U|--username> <-m|--method docker|local> -- [...psql opts and args]

Examples:
        # By default, this command will present interactive instance and user
        # pickers and connect via 'docker exec'
        cp-psql

        # Connect using a specific instance and user
        cp-psql -i storefront-n1-689qacsi -U admin

        # Connect using a locally-running psql client
        PGPASSWORD=password cp-psql -i storefront-n1-689qacsi -U admin -m local

        # Include a '--' separator to pass additional psql args
        cp-psql -i storefront-n1-689qacsi -U admin -- -c 'select 1'

        # Stdin also works
        echo 'select 1' | cp-psql -i storefront-n1-689qacsi -U admin
```

#### `cp-docker-exec`

This function is a shortcut for using `docker exec` on an instance. By default,
it will run `bash` in the instance. Just like `cp-psql`, this function works
with every environment, and its help text demonstrates the intended use:

```
> cp-docker-exec --help

cp-docker-exec [-h|--help]
cp-docker-exec <-i|--instance-id instance id> command [... command args]

Examples:
        # By default, this command will present an interactive instance picker and
        # open a bash shell in the target instance
        cp-docker-exec

        # Open a bash shell on a specific instance
        cp-docker-exec -i storefront-n1-689qacsi

        # Open a bash shell as a specific user
        cp-docker-exec -i storefront-n1-689qacsi -u root

        # Start a command with arguments
        cp-docker-exec -i storefront-n1-689qacsi psql -U admin storefront -c 'select 1'

        # Also works with stdin
        echo 'select 1' | cp-docker-exec -i storefront-n1-689qacsi psql -U admin storefront
```

# `pgedge-cp-env` integration for Oh My Zsh

This directory contains a `pgedge-cp-env` Oh My Zsh plugin that will display the
current `${CP_ENV}` value in your prompt. It also contains a minimal theme,
based on the default Oh My Zsh theme, that uses the plugin.

## Prerequisites

- [Oh My
  Zsh](https://github.com/ohmyzsh/ohmyzsh?tab=readme-ov-file#basic-installation)

## Installation

Run the included installation script:

```sh
# ZSH_CUSTOM is where you can install custom themes and plugins for Oh My Zsh.
# We need to propagate this variable to the script when we run it.
ZSH_CUSTOM=$ZSH_CUSTOM ./hack/pgedge-cp-env.install.zsh
```

## Usage

### Plugin

To use the plugin, include `pgedge-cp-env` in the `plugins` array in your
`~/.zshrc` file. For example:

```sh
plugins=(git pgedge-cp-env)
```

### Theme

To use the included theme, set `ZSH_THEME` to `pgedge-cp-env` in your `~/.zshrc`
file:

```sh
ZSH_THEME="pgedge-cp-env"
```

You can also use this theme as an example for how to use the `pgedge-cp-env`
plugin in your own theme. All of the customization variables are listed at the
top of the `pgedge-cp-env.zsh-theme` file.

#### Switching themes dynamically

If you don't want to use this theme all the time, you can add a function to your
`~/.zshrc` file to switch to it dynamically:

```sh
# Using zsh's hash function to create a shortcut to the control-plane repo
# directory. This lets you refer to the directory as ~control-plane, e.g
# 'cd ~control-plane`
hash -d control-plane=<path to the control-plane directory>

activate-control-plane() {
    # You can also source the dev-env script here
    source ~control-plane/hack/dev-env.zsh

    # Enable the pgedge-cp-env plugin
    plugins=(git pgedge-cp-env)

    # Use the pgedge-cp-env theme
    ZSH_THEME="pgedge-cp-env"

    # Apply the changes by resourcing oh-my-zsh
    source $ZSH/oh-my-zsh.sh
}
```

Then, you can switch themes and activate the `control-plane` development
environment by running:

```sh
activate-control-plane
```
