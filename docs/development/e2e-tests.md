# Automated end-to-end tests

> [!NOTE]
> The end-to-end tests and this document are a work in progress. The
> contents of this document reflect the current state of these tests. We will
> add more sections as we add new functionality.

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
      - [Lima test fixture targets](#lima-test-fixture-targets)
      - [Deploying new code changes](#deploying-new-code-changes)
      - [Testing published releases](#testing-published-releases)
      - [Simulating global deployments](#simulating-global-deployments)
      - [Emulating x86\_64 with Lima](#emulating-x86_64-with-lima)
      - [Stopping and starting hosts](#stopping-and-starting-hosts)
      - [Cleanup](#cleanup)
        - [Tearing down the Control Plane](#tearing-down-the-control-plane)
        - [Tearing down the virtual machines](#tearing-down-the-virtual-machines)
    - [EC2 test fixtures](#ec2-test-fixtures)
      - [EC2 test fixture targets](#ec2-test-fixture-targets)
      - [Deploying new code changes](#deploying-new-code-changes-1)
      - [Testing published releases](#testing-published-releases-1)
      - [Deploying arm64 instances on EC2](#deploying-arm64-instances-on-ec2)
      - [Stopping and starting hosts](#stopping-and-starting-hosts-1)
      - [Cleanup](#cleanup-1)
        - [Tearing down the Control Plane](#tearing-down-the-control-plane-1)
        - [Tearing down the virtual machines](#tearing-down-the-virtual-machines-1)

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

[Installation instructions
page](https://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html)

```sh
pipx install --include-deps ansible
pipx inject ansible 'botocore>=1.34.0'
pipx inject ansible 'boto3>=1.34.0'
```

### Lima test fixtures

> [!IMPORTANT]
> These test fixtures are currently only supported on MacOS with
> Apple Silicon. Lima is supported on Linux, but our configurations are written
> with MacOS in mind.

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

#### Lima test fixture targets

```sh
# Deploy the virtual machines. By default, this will create Rocky 9 VMs with
# Lima. This needs to download a ~500Mb VM image the first time it runs, so it
# may take a while to start the first machine.
make -C e2e/fixtures deploy-lima-machines

# Install prerequisites on VMs
make -C e2e/fixtures setup-lima-hosts

# Build the control-plane binaries
make goreleaser-build

# Deploy the control-plane
make -C e2e/fixtures deploy-lima-control-plane
```

The `deploy-lima-control-plane` target will output a "test config" file that
contains IPs that you can use to contact the virtual machines. This test config
can be used as an input to the end-to-end tests. You can `cat` this file to see
what's in it, for example:

```sh
cat e2e/fixtures/outputs/lima.test_config.yaml
```

You can use the `external_ip` for each host in that file to interact with its
Control Plane instance. For example:

```sh
curl http://192.168.105.2:3000/v1/version
```

You can use the `ssh_command` for each host in that file to SSH to the instance.
For example:

```sh
ssh -F /Users/jasonlynch/.lima/host-1/ssh.config lima-host-1
```

#### Deploying new code changes

If you've made new code changes that you'd like to deploy, you can rerun these
steps:

```sh
# Build the control-plane binaries
make goreleaser-build

# Deploy the control-plane
make -C e2e/fixtures deploy-lima-control-plane
```

#### Testing published releases

If you'd like to test a published image, for example during pre-release testing,
you can skip the image build and override the deployed control-plane image using
`EXTRA_VARS`, for example:

```sh
make -C e2e/fixtures deploy-lima-control-plane EXTRA_VARS='external_control_plane_image=ghcr.io/pgedge/control-plane:v0.2.0-rc.3'
```

#### Simulating global deployments

The test fixtures can add latency between specific virtual machines to simulate
global deployments. The simulated region for each virtual machine is set in the
Ansible vars file for each test fixture. The `lima_rocky_global` test fixture
simulates virtual machines in `us-west-1`, `af-south-1`, and `ap-southeast-4`:

```sh
make -C e2e/fixtures setup=lima-hosts VARS_FILE=vars/lima_rocky_global.yaml
```

#### Emulating x86_64 with Lima

> [!WARNING]
> This is extremely slow, like 10+ minutes to complete a deployment,
> and should only be done in the absence of better options.

You can use `EXTRA_VARS` to override the detected architecture:

```sh
make -C e2e/fixtures deploy-lima-machines EXTRA_VARS='architecture=x86_64'
make -C e2e/fixtures setup-lima-hosts EXTRA_VARS='architecture=x86_64'
make -C e2e/fixtures deploy-lima-control-plane EXTRA_VARS='architecture=x86_64'
```

#### Stopping and starting hosts

You can stop the hosts without tearing them down using the `stop-lima-machines`
target:

```sh
make -C e2e/fixtures stop-lima-machines
```

You can start them again by re-running the `deploy-lima-machines` target. It's
also recommended to re-run the `deploy-lima-control-plane` target afterwards to
ensure that the test config is up-to-date.

```sh
make -C e2e/fixtures deploy-lima-machines
make -C e2e/fixtures deploy-lima-control-plane
```

#### Cleanup

##### Tearing down the Control Plane

You can tear down the Control Plane in the test fixtures as well as any
databases its deployed with this `make` target:

```sh
make -C e2e/fixtures teardown-lima-control-plane
```

This can be a faster alternative to remaking the virtual machines if you're
re-testing the cluster initialization flow.

##### Tearing down the virtual machines

To completely remove the virtual machines, do:

```sh
make -C e2e/fixtures teardown-lima-machines
```

### EC2 test fixtures

The EC2 test fixtures deploy the following resources:

- An S3 bucket
- An IAM role with permission to manage objects in the bucket
- An instance profile that uses the IAM role
- A security group that grants access to all ports to the test runner's IP
  address
- EC2 instances

Most resources, including the global resources, are scoped to single region to
avoid conflicts between fixtures deployed to different regions. We use `boto3`
to find the currently-configured region, so you have multiple options to set the
deployment region, such as setting the region in your AWS profile or setting the
`AWS_DEFAULT_REGION` environment variable. See the [AWS CLI
docs](https://docs.aws.amazon.com/cli/v1/userguide/cli-configure-files.html) for
more more information on setting a region.

Note that we leave the S3 bucket, IAM resources, and the security group in place
in the teardown plays because these resources are relatively low-cost and
because it makes it easier to switch between fixtures, such as deploying a
different architecture.

#### EC2 test fixture targets

> [!NOTE]
> Sometimes, AWS takes too long to provision a public IP and you'll see
> an error like `'dict object' has no attribute 'public_ip_address'`. These
> plays are idempotent, so you can safely rerun it if you see a failure. We're
> already using the available mechanism to wait for the instance to be ready, so
> we may need some new approaches if this is a common problem.

```sh
# Deploy the virtual machines. By default, this will create Rocky 9 VMs with
# x86_64 architecture.
make -C e2e/fixtures deploy-ec2-machines

# Install prerequisites on VMs
make -C e2e/fixtures setup-ec2-hosts

# Build the control-plane binaries
make goreleaser-build

# Deploy the control-plane
make -C e2e/fixtures deploy-ec2-control-plane
```

The `deploy-ec2-control-plane` target will output a "test config" file that
contains IPs that you can use to contact the virtual machines. This test config
can be used with the end-to-end tests. You can `cat` this file to see what's in
it, for example:

```sh
cat e2e/fixtures/outputs/ec2.test_config.yaml
```

You can use the `external_ip` for each host in that file to interact with its
Control Plane instance. For example:

```sh
curl http://3.133.148.76:3000/v1/version
```

You can use the `ssh_command` for each host in that file to SSH to the instance.
For example:

```sh
ssh -l rocky -i /Users/jasonlynch/workspace/pgEdge/control-plane/e2e/fixtures/outputs/ec2_deploy 3.133.148.76
```

#### Deploying new code changes

If you've made new code changes that you'd like to deploy, you can rerun these
steps:

```sh
# Build the control-plane binaries
make goreleaser-build

# Deploy the control-plane
make -C e2e/fixtures deploy-ec2-control-plane
```

#### Testing published releases

If you'd like to test a published image, for example during pre-release testing,
you can skip the image build and override the deployed control-plane image using
`EXTRA_VARS`, for example:

```sh
make -C e2e/fixtures deploy-ec2-control-plane EXTRA_VARS='external_control_plane_image=ghcr.io/pgedge/control-plane:v0.2.0-rc.3'
```

#### Deploying arm64 instances on EC2

You can use `EXTRA_VARS` to override the detected architecture:

```sh
make -C e2e/fixtures deploy-ec2-machines EXTRA_VARS='architecture=arm64'
make -C e2e/fixtures setup-ec2-hosts EXTRA_VARS='architecture=arm64'      
make -C e2e/fixtures deploy-ec2-control-plane EXTRA_VARS='architecture=arm64'
```

#### Stopping and starting hosts

You can stop the hosts without tearing them down using the `stop-ec2-machines`
target. Stopped hosts do not incur an instance charge (you're only charged for
the EBS storage) so this is a useful cost-saving measure:

```sh
make -C e2e/fixtures stop-ec2-machines

# It's important to include the `EXTRA_VARS` if you've deployed arm64 instances:
make -C e2e/fixtures stop-ec2-machines EXTRA_VARS='architecture=arm64'
```

You can start them again by re-running the `deploy-ec2-machines` target. It's
also recommended to re-run the `deploy-ec2-control-plane` target afterwards to
ensure that the test config is up-to-date, because the public IPs can change.

```sh
make -C e2e/fixtures deploy-ec2-machines
make -C e2e/fixtures deploy-ec2-control-plane

# Like above, be sure to include the `EXTRA_VARS` if you've deployed arm64
# instances:
make -C e2e/fixtures deploy-ec2-machines EXTRA_VARS='architecture=arm64'
make -C e2e/fixtures deploy-ec2-control-plane EXTRA_VARS='architecture=arm64'
```

#### Cleanup

##### Tearing down the Control Plane

You can tear down the Control Plane in the test fixtures as well as any
databases its deployed with this `make` target:

```sh
make -C e2e/fixtures teardown-ec2-control-plane

# It's important to include the `EXTRA_VARS` if you've deployed arm64 instances:
make -C e2e/fixtures teardown-ec2-control-plane EXTRA_VARS='architecture=arm64'
```

This can be a faster alternative to remaking the virtual machines if you're
re-testing the cluster initialization flow.

##### Tearing down the virtual machines

To completely remove the virtual machines, do:

```sh
make -C e2e/fixtures teardown-ec2-machines

# It's important to include the `EXTRA_VARS` if you've deployed arm64 instances:
make -C e2e/fixtures teardown-ec2-machines EXTRA_VARS='architecture=arm64'
```
