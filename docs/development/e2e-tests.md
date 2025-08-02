# Automated end-to-end tests

> [!NOTE]
> The end-to-end tests and this document are a work in progress. The contents of
> this document reflect the current state of these tests. We will add more
> sections as we add new functionality.

This document describes the API-driven end-to-end tests for the Control Plane.
These tests can be run against any set of Control Plane instances, including the
ones we run through Docker Compose. We also have "test fixtures", which are
Control Plane instances running on virtual machines.

- [Automated end-to-end tests](#automated-end-to-end-tests)
  - [Test fixtures](#test-fixtures)
    - [Common prerequisites](#common-prerequisites)
      - [`pipx`](#pipx)
      - [Ansible](#ansible)
    - [Lima test fixtures](#lima-test-fixtures)
      - [Prerequisites](#prerequisites)
        - [Lima](#lima)
        - [`socket_vmnet` (only needed for x86\_64 emulation)](#socket_vmnet-only-needed-for-x86_64-emulation)
    - [Running the test fixtures](#running-the-test-fixtures)
    - [Deploying new code changes](#deploying-new-code-changes)
    - [Testing published releases](#testing-published-releases)
    - [Simulating global deployments](#simulating-global-deployments)
    - [SSH'ing to Lima VMs](#sshing-to-lima-vms)
    - [Emulating x86\_64 with Lima](#emulating-x86_64-with-lima)
    - [Cleanup](#cleanup)
      - [Tearing down the Control Plane](#tearing-down-the-control-plane)
      - [Tearing down the virtual machines](#tearing-down-the-virtual-machines)

## Test fixtures

We deploy test fixtures using Ansible, which we invoke through `make` targets.

### Common prerequisites

These prerequisites are common to all types of test fixtures.

#### `pipx`

`pipx` is a tool that runs Python programs in isolated environments. It's the
recommended way to run Ansible, which we use to deploy the test fixtures.

[Homepage](https://pipx.pypa.io/stable/)

```sh
brew install pipx
pipx ensurepath
sudo pipx ensurepath --global # optional to allow pipx actions with --global argument
```

Be sure to restart your terminal session after running the `ensurepath` commands
so that the profile changes take effect.

#### Ansible

We're using Ansible to configure the test fixtures and install the Control Plane
and other software on them.

[Installation instructions page](https://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html)

```sh
pipx install --include-deps ansible
```

### Lima test fixtures

> [!IMPORTANT]
> These test fixtures are currently only supported on MacOS with Apple Silicon.
> Lima is supported on Linux, but our configurations are written with MacOS in
> mind.

#### Prerequisites

##### Lima

Lima is an easy-to-use virtual machine runner that works well on MacOS.

```sh
# Installation through homebrew

brew install lima
```

##### `socket_vmnet` (only needed for x86_64 emulation)

In order to use x86_64 emulation, you must also install `socket_vmnet`.

```sh
VERSION="$(curl -fsSL https://api.github.com/repos/lima-vm/socket_vmnet/releases/latest | jq -r .tag_name)"
FILE="socket_vmnet-${VERSION:1}-$(uname -m).tar.gz"

# Download the binary archive
curl -OSL "https://github.com/lima-vm/socket_vmnet/releases/download/${VERSION}/${FILE}"

# Install /opt/socket_vmnet from the binary archive
sudo tar Cxzvf / "${FILE}" opt/socket_vmnet

# Add lima entries to sudoers so that it can invoke socket_vmnet
limactl sudoers | sudo tee /etc/sudoers.d/lima
```

### Running the test fixtures

The test fixtures are automated through `make`.

```sh
# Deploy the virtual machines. By default, this will create Rocky 9 VMs with
# Lima. This needs to download a ~500Mb VM image the first time it runs, so it
# may take a while to start the first machine.
make -C e2e/fixtures deploy-machines

# Install prerequisites on VMs
make -C e2e/fixtures setup-hosts

# Build the control-plane binaries
make goreleaser-build

# Deploy the control-plane
make -C e2e/fixtures deploy-control-plane
```

The `deploy-control-plane` target will output a "test config" file that contains
IPs that you can use to contact the virtual machines. This test config can be
used as an input to the end-to-end tests. You can `cat` this file to see what's
in it, for example:

```sh
cat e2e/fixtures/outputs/lima.test_config.yaml
```

You can use the `external_ip` for each host in that file to interact with each
Control Plane instance. For example:

```sh
curl http://192.168.105.2:3000/v1/cluster/init
```

### Deploying new code changes

If you've made new code changes that you'd like to deploy, you can rerun these
steps:

```sh
# Build the control-plane binaries
make goreleaser-build

# Deploy the control-plane
make -C e2e/fixtures deploy-control-plane
```

### Testing published releases

If you'd like to test a published image, for example during pre-release testing,
you can skip the image build and override the deployed control-plane image using
`EXTRA_VARS`, for example:

```sh
make -C e2e/fixtures deploy-control-plane EXTRA_VARS='external_control_plane_image=ghcr.io/pgedge/control-plane:v0.2.0-rc.3'
```

### Simulating global deployments

The test fixtures can add latency between specific virtual machines to simulate
global deployments. The simulated region for each virtual machine is set in the
Ansible vars file for each test fixture. The `lima_rocky_global` test fixture
simulates virtual machines in `us-west-1`, `af-south-1`, and `ap-southeast-4`:

```sh
make -C e2e/fixtures setup-hosts VARS_FILE=vars/lima_rocky_global.yaml
```

### SSH'ing to Lima VMs

You can use `ssh` to connect to each Lima VM. For example:

```sh
# Connecting to host-1
ssh -F ~/.lima/host-1/ssh.config lima-host-1

# Connecting to host-2
ssh -F ~/.lima/host-2/ssh.config lima-host-2

# Connecting to host-3
ssh -F ~/.lima/host-3/ssh.config lima-host-3
```

### Emulating x86_64 with Lima

> [!WARNING]
> This is extremely slow, like 10+ minutes to complete a deployment, and should
> only be done in the absence of better options.

You can use `EXTRA_VARS` to override the detected architecture:

```sh
make -C e2e/fixtures deploy-machines EXTRA_VARS='architecture=x86_64'
```

### Cleanup

#### Tearing down the Control Plane

You can tear down the Control Plane in the test fixtures as well as any
databases its deployed with this `make` target:

```sh
make -C e2e/fixtures teardown-control-plane
```

This can be a faster alternative to remaking the virtual machines if you're
re-testing the cluster initialization flow.

#### Tearing down the virtual machines

To completely remove the virtual machines, do:

```sh
make -C e2e/fixtures teardown-machines
```
