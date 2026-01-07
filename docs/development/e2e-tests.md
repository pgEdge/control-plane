# Automated end-to-end tests

This document describes the API-driven end-to-end tests for the Control Plane.
These tests can be run against any set of Control Plane servers, including the
ones we run through Docker Compose. We also have "test fixtures", which are
Control Plane servers running on virtual machines.

> [!NOTE]
> The end-to-end tests and this document are a work in progress. The contents of
> this document reflect the current state of these tests. We will add more
> sections as we add new functionality.

## End-to-end tests

The end-to-end (E2E) tests live in the top-level `e2e` package. By default,
these tests are configured to run against a locally-running Control Plane
cluster, but they can also be run against remote clusters and other test
fixtures.

The `TestMain` for this package starts by initializing the Control Plane cluster
with our high-level `client` library, which makes them usable against both
initialized and uninitialized clusters. The drawback of this decision is that we
have limited ability to test cluster management features, such as adding or
removing hosts. Instead, we've prioritized a fast-feedback loop for database
management features.

### Running the tests against Docker compose

By default, the tests will run against a locally-running Control Plane cluster
using the same settings as our Docker Compose setup. To run the tests, first
start the Control Plane servers using either the `dev-watch` or `dev-detach`
make targets:

```sh
make dev-watch
```

Then, use the `test-e2e` target in a separate terminal session:

```sh
make test-e2e
```

You'll notice that some tests, such as those that depend on S3, are skipped.
This is because each test fixture has a set of supported features that we check
at the start of each test.

### Running the tests against a test fixture

If you have a running test fixture, as described in the [Test
fixtures](#test-fixtures) section below, you can target it by setting the
`E2E_FIXTURE` environment variable. For example, to run the tests against the
Lima test fixture:

```sh
make test-e2e E2E_FIXTURE=lima
```

Or to run against the EC2 test fixture:

```sh
make test-e2e E2E_FIXTURE=ec2
```

The test fixture name is used to find the "test config" YAML file in the
`e2e/fixtures/outputs` directory. For example, the `lima` test fixture name
corresponds to the `e2e/fixtures/outputs/lima.test_config.yaml` test config
file. These files are generated when you deploy the control plane to the test
fixture.

### Additional test options

The `test-e2e` target supports a few other environment variables besides
`E2E_FIXTURE`:

- `E2E_PARALLEL`: Sets the test parallelism. By default, the tests are
  configured to run in parallel, and `go test` will run up to `GOMAXPROCS` tests
  at once. An example usage of this variable is setting it to 1 to make tests
  run sequentially:

```sh
make test-e2e E2E_PARALLEL=1
```

- `E2E_RUN`: Sets the `-run` go test option. This is a regular expression that
  limits the tests to those that match the expression. For example, to use this
  option to run only the `TestPosixBackupRestore` test:

```sh
make test-e2e E2E_RUN=TestPosixBackupRestore
```

- `E2E_SKIP_CLEANUP`: Setting this to 1 will skip the cleanup operations that
  run at the end of the tests. This can be useful if you're debugging a
  particular test. For example, using it with the `E2E_RUN` variable to leave
  the database and local Posix repository in place:

```sh
make test-e2e E2E_RUN=TestPosixBackupRestore E2E_SKIP_CLEANUP=1
```

- `E2E_DEBUG`: Setting this to 1 will make the tests output debug information
  for failed tests to `./e2e/debug`:

```sh
make test-e2e E2E_DEBUG=1
```

- `E2E_DEBUG_DIR`: Allows you to override the directory used by the `E2E_DEBUG`
  feature:

```sh
make test-e2e E2E_DEBUG=1 E2E_DEBUG_DIR=/tmp/e2e-debug
```

### Writing new tests

The `e2e` package contains several helpers that make it easier to interact with
the Control Plane API. Your tests can interact with the API and other host
functions via the `fixture` global variable. You can also use this variable to
create a `DatabaseFixture`, which is a wrapper around the API that makes it easy
to write database lifecycle tests. See the backup and restore tests in
`e2e/backup_restore_test.go` for examples of how to use these helpers.

## Test fixtures

We deploy test fixtures using Ansible, which we invoke through `make` targets.

### Common prerequisites

These prerequisites are common to all types of test fixtures.

#### Project-level tools

Make sure you've installed the project-level tools with:

```sh
make install-tools
```

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
# Build and deploy the Control Plane to Lima virtual machines. By default, this
# will create six Rocky 9 VMs with Lima. This needs to download a ~500Mb VM
# image the first time it runs, so it may take a while to start the first
# machine.
make deploy-lima-fixture

# Rebuild and redeploy the Control Plane to existing Lima virtual machines
make update-lima-fixture

# Teardown, rebuild, and redeploy the Control Plane to existing Lima virtual
# machines
make reset-lima-fixture

# Teardown the Lima virtual machines
make teardown-lima-fixture
```

The `{deploy,update,reset}-lima-fixture` targets will output a "test config"
file that contains IPs that you can use to contact the virtual machines. This
test config can be used as an input to the end-to-end tests. You can `cat` this
file to see what's in it, for example:

```sh
cat e2e/fixtures/outputs/lima.test_config.yaml
```

You can use the `external_ip` for each host in that file to interact with its
Control Plane server. For example:

```sh
curl http://192.168.105.2:3000/v1/version
```

You can use the `ssh_command` for each host in that file to SSH to the instance.
For example:

```sh
ssh -F /Users/jasonlynch/.lima/host-1/ssh.config lima-host-1
```

#### Emulating x86_64 with Lima

> [!WARNING]
> This is extremely slow, like 10+ minutes to complete a deployment,
> and should only be done in the absence of better options.

You can use `FIXTURE_EXTRA_VARS` with the
`{deploy,update,reset,teardown}-lima-fixture` targets to override the detected
architecture:

```sh
make deploy-lima-fixture FIXTURE_EXTRA_VARS='architecture=x86_64'

# It's important to include the architecture with every other target as well:
make update-lima-fixture FIXTURE_EXTRA_VARS='architecture=x86_64'
make reset-lima-fixture FIXTURE_EXTRA_VARS='architecture=x86_64'
make teardown-lima-fixture FIXTURE_EXTRA_VARS='architecture=x86_64'
```

#### Stopping and starting hosts

You can stop the hosts without tearing them down using the `stop-lima-machines`
target:

```sh
make stop-lima-fixture
```

You can start them again by re-running the `deploy-lima-fixture` target.

```sh
make deploy-lima-fixture
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

```sh
# Build and deploy the Control Plane to EC2 virtual machines. By default, this
# will create six Rocky 9 VMs with x86_64 architecture.
make deploy-ec2-fixture

# Rebuild and redeploy the Control Plane to existing EC2 virtual machines
make update-ec2-fixture

# Teardown, rebuild, and redeploy the Control Plane to existing EC2 virtual
# machines
make reset-ec2-fixture

# Teardown the EC2 virtual machines
make teardown-ec2-fixture
```

The `{deploy,update,reset}-ec2-fixture` targets will output a "test config" file
that contains IPs that you can use to contact the virtual machines. This test
config can be used with the end-to-end tests. You can `cat` this file to see
what's in it, for example:

```sh
cat e2e/fixtures/outputs/ec2.test_config.yaml
```

You can use the `external_ip` for each host in that file to interact with its
Control Plane server. For example:

```sh
curl http://3.133.148.76:3000/v1/version
```

You can use the `ssh_command` for each host in that file to SSH to the instance.
For example:

```sh
ssh -l rocky -i /Users/jasonlynch/workspace/pgEdge/control-plane/e2e/fixtures/outputs/ec2_deploy 3.133.148.76
```

#### Deploying arm64 instances on EC2

You can use `FIXTURE_EXTRA_VARS` with the
`{deploy,update,reset,teardown}-ec2-fixture` targets to override the default
architecture:

```sh
make deploy-ec2-fixture FIXTURE_EXTRA_VARS='architecture=arm64'

# It's important to include the architecture with every other target as well:
make update-ec2-fixture FIXTURE_EXTRA_VARS='architecture=arm64'
make reset-ec2-fixture FIXTURE_EXTRA_VARS='architecture=arm64'
make teardown-ec2-fixture FIXTURE_EXTRA_VARS='architecture=arm64'
```

#### Stopping and starting hosts

You can stop the hosts without tearing them down using the `stop-ec2-fixture`
target. Stopped hosts do not incur an instance charge (you're only charged for
the EBS storage) so this is a useful cost-saving measure:

```sh
make stop-ec2-fixture

# The architecture is incorporated into the instance's identifier, so it's
# important to include `FIXTURE_EXTRA_VARS` if you've deployed arm64 instances:
make stop-ec2-fixture FIXTURE_EXTRA_VARS='architecture=arm64'
```

You can start them again by re-running the top-level `deploy-ec2-fixture`
target:

```sh
make deploy-ec2-fixture

# Similar to the above, be sure to include the `FIXTURE_EXTRA_VARS` if you've
# deployed arm64 instances:
make deploy-ec2-fixture FIXTURE_EXTRA_VARS='architecture=arm64'
```

### Testing published releases

If you'd like to test a published image, for example during pre-release testing,
you can skip the image build and override the deployed control-plane image by
specifying `FIXTURE_CONTROL_PLANE_IMAGE` with the
`{deploy,update,reset}-{lima,ec2}-fixture` targets. For example:

```sh
make update-lima-fixture FIXTURE_CONTROL_PLANE_IMAGE='ghcr.io/pgedge/control-plane:v0.6.2-rc.1'
```

### Fixture variants

By default, we deploy the `large` fixture variant, which contains six virtual
machines and Control Plane instances, where three are configured to act as Etcd
servers and three are configured to act as clients only. You can specify a
different variant using the `FIXTURE_VARIANT` variable with the
`{deploy,update,reset,teardown}-{lima,ec2}-fixture` targets. For example:

```sh
# Deploy the 'small' fixture variant with lima
make deploy-lima-fixture FIXTURE_VARIANT=small

# After it's deployed, it's used the same as the normal lima fixture
make test-e2e E2E_FIXTURE=lima

# Make sure to specify the variant with the other targets as well
make update-lima-fixture FIXTURE_VARIANT=small
make reset-lima-fixture FIXTURE_VARIANT=small
make teardown-lima-fixture FIXTURE_VARIANT=small
```

> [!NOTE]
> Make sure to run the `teardown-{lima,ec2}-fixture` before switching variants.

#### Available variants

- `small`: Contains three hosts, all with `ETCD_MODE=server`
- `large` (default): Contains six hosts, three with `ETCD_MODE=server` and three
  with `ETCD_MODE=client`
- `huge`: Contains twelve hosts, five with `ETCD_MODE=server` and seven with
  `ETCD_MODE=client`

There are also "global" variants that mirror the above variants but use `tc` to
introduce artificial latency between the hosts that simulates the latencies
between the `us-west-1`, `af-south-1`, and `ap-southeast-4` AWS regions.

- `small_global`
- `large_global`
- `huge_global`

### Custom test fixtures

You can create custom test fixtures by writing new test config YAML files with
the naming scheme `<fixture name>.test_config.yaml` to `e2e/fixtures/outputs`.
For example, if you would like to run the S3 E2E tests using your local Docker
Compose configuration, you could create a test config file such as:

```yaml
---
hosts:
  host-1:
    external_ip: 127.0.0.1
    port: 3000
  host-2:
    external_ip: 127.0.0.1
    port: 3001
  host-3:
    external_ip: 127.0.0.1
    port: 3002
s3:
  enabled: true
  bucket: <an existing S3 bucket in your account>
  access_key_id: <your access key ID>
  secret_access_key: <your secret access key>
  region: <your bucket's region>
```

and save it as `e2e/fixtures/local_s3.test_config.yaml`. Then, you can provide
the fixture name `local_s3` to the `test-e2e` Make target:

```sh
make test-e2e E2E_FIXTURE=local_s3
```
