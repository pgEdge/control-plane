# Running a local Control Plane cluster in virtual machines

This repository contains scripts and configurations to run a three-node Docker
Swarm cluster with Vagrant. You can run Control Plane on any or all of these
machines to test different configurations.

- [Running a local Control Plane cluster in virtual machines](#running-a-local-control-plane-cluster-in-virtual-machines)
  - [Apple Silicon](#apple-silicon)
    - [Prerequisites](#prerequisites)
      - [VMware Fusion Pro 13](#vmware-fusion-pro-13)
      - [Vagrant](#vagrant)
      - [`pipx`](#pipx)
      - [Ansible](#ansible)
      - [Restish](#restish)
  - [Initialize the cluster](#initialize-the-cluster)
  - [Start and populate the local image registry](#start-and-populate-the-local-image-registry)
  - [Start the Control Plane server on each instance](#start-the-control-plane-server-on-each-instance)
    - [Why do we need `sudo`?](#why-do-we-need-sudo)
  - [Interact with the Control Plane API](#interact-with-the-control-plane-api)
  - [Resetting each instance](#resetting-each-instance)
  - [Teardown](#teardown)
  - [Running Ansible](#running-ansible)

## Apple Silicon

There is a very narrow range of support for Vagrant on Apple Silicon. The only
virtualization platform that has consistently worked in my testing is VMWare
Fusion. I've also found that many boxes marked as arm64 are actually built on
amd64 hosts and are therefore incompatible with Apple Silicon machines.

These tools and instructions are carefully chosen based on what worked for me.

### Prerequisites

#### VMware Fusion Pro 13

[Download link](https://support.broadcom.com/group/ecx/productdownloads?subfamily=VMware+Fusion)

- Requires you to create a Broadcom Account and fill out an additional form
  with your home address.
- I just picked the latest release.
- Fusion Pro was recently made free for personal use. If we prove this approach
  for more employees, we'll invest in business licenses.

> [!IMPORTANT]
> You must restart your computer after installing VMware Fusion so that MacOS
> will load the new system extensions. Otherwise, you won't be able to start the
> Vagrant boxes.

#### Vagrant

```sh
brew tap hashicorp/tap
brew install hashicorp/tap/hashicorp-vagrant

# Then, install the vagrant-vmware-desktop plugin
vagrant plugin install vagrant-vmware-desktop
```

You'll also need to install an additional set of utilities for Vagrant to
communicate with VMware Fusion.

Use the ARM64 `Binary Download` link on this page:
https://developer.hashicorp.com/vagrant/install/vmware

> [!WARNING]  
> Ignore the Homebrew instructions on this page. They describe how to install
> Vagrant, not the utilities.

#### `pipx`

`pipx` is a tool that runs Python programs in isolated environments. It's the
recommended way to run Ansible, which we use to configure the Vagrant boxes.

[Homepage](https://pipx.pypa.io/stable/)

```sh
brew install pipx
pipx ensurepath
sudo pipx ensurepath --global # optional to allow pipx actions with --global argument
```

Be sure to restart your terminal session after running the `ensurepath` commands
so that the profile changes take effect.

#### Ansible

We're using Ansible to configure the Vagrant boxes and install software on them.

[Installation instructions page](https://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html)

```sh
pipx install --include-deps ansible
```

#### Restish

Restish is a CLI tool to interact with REST APIs that expose an OpenAPI spec,
like the Control Plane API. It's not strictly required, but it is recommended.

[Installation guide](https://rest.sh/#/guide)

```sh
brew install rest-sh/tap/restish
```

It's recommended to add this environment variable to your `.zshrc` as well to
disable Restish's default retry behavior:

```sh
export RSH_RETRY=0
```

After you've added this line, you can run `exec zsh` to reload the configuration
in your current shell session. It will automatically apply to any new sessions.

After installation, modify the Restish configuration file to add entries for the
local Control Plane instances. On MacOS, this file will be
`~/Library/Application Support/restish/apis.json`. See
[the configuration page](https://rest.sh/#/configuration) to find the
configuration file location for non-MacOS systems.

```json
{
  "$schema": "https://rest.sh/schemas/apis.json",
  "control-plane-vm-1": {
    "base": "http://10.1.0.11:3000",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-vm-2": {
    "base": "http://10.1.0.12:3000",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  },
  "control-plane-vm-3": {
    "base": "http://10.1.0.13:3000",
    "profiles": {
      "default": {
        
      }
    },
    "tls": {}
  }
}
```

## Initialize the cluster

Once all of the prerequisites are complete, you can initialize the Vagrant
boxes:

```sh
make vagrant-init
```

It will take a few minutes to provision and configure the new machines.

> [!NOTE]  
> In case you stop the machines, for example by running `vagrant halt` or
> restarting your computer, use `make vagrant-up` to start them again. This
> target starts the machines in parallel, so it saves a few minutes over running
> `vagrant up` by itself.

## Start and populate the local image registry

We're using a local Docker image registry to avoid the time-consuming (and
potentially expensive) process of pushing development images to a public
repository. In order to run the local registry:

```sh
# Note: this only needs to be done once. The registry will start up whenever
# you start the docker daemon until you remove the container.
make start-local-registry
```

Next, initialize a new buildx builder for control-plane:

```sh
# Note: this only needs to be done once:
make buildx-init
```

Finally, build and push the pgEdge Postgres images to your local registry:

```sh
# Should be repeated any time you make changes to the image, or if you recreate
# your registry.
make pgedge-images
```

It will take a few minutes to build and push the images.

## Start the Control Plane server on each instance

First, make sure to build the server binary from your host machine:

```sh
GOOS=linux go build -o ./control-plane ./server
```

Each machine will have a few directories and files already configured:

- `~/control-plane`: The control plane project will sync to this directory.
- `~/control-plane/dev`: Contains preset configuration files for each machine.
- `/opt/pgedge/data`: Per the configuration files, each machine will store its
  data in this directory. The control plane needs to be able to set ownership
  on each database's data directory, so, unfortunately, this directory can't be
  synced to the host machine.

Using three separate terminal windows, connect to each vagrant machine and run
the Control Plane server:

```sh
make ssh-1
cd control-plane
sudo ./control-plane run -p -c ./dev/${HOSTNAME}.json
```

```sh
make ssh-2
cd control-plane
sudo ./control-plane run -p -c ./dev/${HOSTNAME}.json
```

```sh
make ssh-3
cd control-plane
sudo ./control-plane run -p -c ./dev/${HOSTNAME}.json
```

### Why do we need `sudo`?

The Control Plane needs to do a few privileged operations when it's configured
for the Docker Swarm orchestrator:

- Changing the ownership of files so they can be managed by the `pgedge` user in
  the pgEdge database containers.
- Reading/writing files that are owned by other users so that we can update and
  delete files after we've changed their ownership.

When we run the Control Plane server with Docker, we use Linux capabilities to
grant these privileges in a more fine-grained way.  We can do the same for the
`control-plane` binary with `setcap` or `capsh`, but for development it's easier
to just use `sudo`. Later on when we develop a system package and a `systemd`
unit for the Control Plane server, we'll use the `systemd`'s capability
functions to assign these capabilities like we do in the Docker setup.

## Interact with the Control Plane API

Now, you should be able to interact with the API using restish. For example, to
initialize a new cluster and create a new database:

```sh
restish control-plane-local-1 init-cluster
restish control-plane-local-2 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-3 join-cluster "$(restish control-plane-local-1 get-join-token)"
restish control-plane-local-1 create-database '{"spec":{"database_name":"my_app","port":5432,"database_users":[{"username":"admin","password":"password","attributes":["SUPERUSER","LOGIN"]},{"username":"app","password":"password","attributes":["LOGIN"],"roles":["pgedge_application"]}],"nodes":[{"name":"n1","host_ids":["host-1"]},{"name":"n2","host_ids":["host-2"]},{"name":"n3","host_ids":["host-3"]}]}}'
```

> [!NOTE] For MacOS users
> If you're running a recent version of MacOS, you might be prompted to grant
> local network access to your terminal program when you run these commands. You
> must allow that access for `restish` to be able to contact the VMs. If you do
> not grant it, you'll likely see a `no route to host` error or similar. If you
> run into this problem, you can grant local network access after the fact by
> doing the following: open the `System Settings` application, clicking `Privacy
> and Security` on the left-hand side, scroll down and click on `Local Network`,
> then make sure that your terminal program (e.g. iTerm2, VSCode, etc.) is
> enabled in the list of applications.

It will take a minute or two to create the database, so don't be alarmed if you
don't see new log messages during this time. Once you see the
`database created successfully` log message, your database will be accessible on
port 5432 of each machine. So from your host machine, you can do:

```sh
PGPASSWORD=password psql -h 10.1.0.11 -p 5432 -U admin my_app
PGPASSWORD=password psql -h 10.1.0.12 -p 5432 -U admin my_app
PGPASSWORD=password psql -h 10.1.0.13 -p 5432 -U admin my_app
```

## Resetting each instance

There's not a way to delete databases yet. To return each machine back to its
initial state:

1. Stop the `control-plane` instance on each machine with `ctrl+c`
2. Remove docker services and networks (can be done from any of the machines):

```sh
docker service ls | tail -n +2 | awk '{ print $2 }' | xargs docker service rm
docker network ls | grep '\-database' | awk '{ print $2 }' | xargs docker network rm
```

3. Delete the `data` directory:

```sh
sudo rm -rf /opt/pgedge/data
```

4. Remove `/etc/fstab` entries if you've used the `loop_device` storage class.

## Teardown

In order to completely tear down all instances, run:

```sh
vagrant destroy -f
```

## Running Ansible

If you need to make changes to the Ansible playbook, you can run it with:

```sh
ansible-playbook playbook.yaml
```

You can also run simple, ad-hoc commands against machines with the `ansible`
command:

```sh
# List all docker services from control-plane-1
ansible control-plane-1 -a 'docker service ls'

# Kill the control-plane processes on all machines
ansible all -a 'pkill control-plane'
```
