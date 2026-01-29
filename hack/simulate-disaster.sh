#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -x

script_dir=$( cd -- "$(dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)
fixtures_dir="${script_dir}/../e2e/fixtures"
fixture_variant="${FIXTURE_VARIANT:-large}"
fixture_extra_vars="${FIXTURE_EXTRA_VARS}"

# Simulates losing a Docker Swarm node, but retains all Etcd data so that we can
# focus on just the Docker Swarm and database instance recovery steps
simulate_swarm_node_loss() {
	local host_id

	for host_id in $@; do
		echo "=== simulating swarm node loss on ${host_id} ==="
		echo

		ssh -T -F ~/.lima/${host_id}/ssh.config lima-${host_id} <<-'EOF'
			if [[ $(docker info --format '{{.Swarm.LocalNodeState}}') == "active" ]]; then
				docker swarm leave --force
			else
				echo "node already left swarm"
			fi
			echo "removing instances data directory"
			sudo rm -rf /data/control-plane/instances
EOF
		echo
	done
}

# Simulates losing an Etcd node, but retains Docker Swarm so that we can focus
# on just the Control Plane and database instance recovery steps
simulate_etcd_node_loss() {
	local host_id

	for host_id in $@; do
		echo "=== simulating etcd node loss on ${host_id} ==="
		echo

		# We're using xargs here to gracefully ignore when the service does not
		# exist
		ssh -T -F ~/.lima/${host_id}/ssh.config lima-${host_id} <<-EOF
			echo "removing control-plane swarm service"
			docker service ls \
				--filter 'name=control-plane_${host_id}' \
				--format '{{ .Name }}' \
				| xargs -r docker service rm
			echo "removing control-plane data directory"
			sudo rm -rf /data/control-plane
EOF
		echo
	done
}

# This is most similar to a real disaster recovery scenario. We're losing the
# entire machine as well as all of its storage. Whatever replacement machine we
# start up may or may not have the same IP address.
simulate_full_loss() {
	local host_id

	for host_id in $@; do
		echo "=== simulating full loss of ${host_id} ==="
		echo

		limactl stop ${host_id}
		limactl delete ${host_id}

		echo
	done
}

# Resets Swarm and the Control Plane on all hosts and returns the Control Plane
# to an uninitialized state.
reset() {
	echo "=== resetting all hosts ==="
	echo

	VARIANT="${fixture_variant}" \
	EXTRA_VARS="${fixture_extra_vars}" \
	make -C "${fixtures_dir}" \
		deploy-lima-machines

	for host_id in $(limactl ls | awk '$1~/^host-/ && $2 == "Running" { print $1 }'); do
		echo "resetting swarm on ${host_id}"

		ssh -T -F ~/.lima/${host_id}/ssh.config lima-${host_id} <<-'EOF'
			if [[ $(docker info --format '{{.Swarm.LocalNodeState}}') == "active" ]]; then
				docker swarm leave --force
			else
				echo "node already left swarm"
			fi
			echo "removing control-plane data directory"
			sudo rm -rf /data/control-plane
		EOF
	done

	VARIANT="${fixture_variant}" \
	EXTRA_VARS="${fixture_extra_vars}" \
	make -C "${fixtures_dir}" \
		setup-lima-hosts \
		teardown-lima-control-plane \
		deploy-lima-control-plane
}

usage() {
cat <<EOF
Usage: $1 <swarm|etcd|full> <host-id> [host-id ...]

Simulates disasters against the Lima test fixtures. Supports three different
different types of disasters to enable us to develop some recovery steps in
parallel:

- swarm: simulates losing a Swarm node and database instance data without losing
  Etcd data
- etcd: simulates losing a Control Plane/Etcd instance without losing Swarm
  quorum.
- full: simulates losing an entire host, affecting both Swarm and Control
  Plane/Etcd.

NOTE: This is only intended to be run against swarm manager/etcd server hosts.

Examples:
	# Simulating losing Swarm on one host
	$1 swarm host-1

	# Simulate losing Swarm on two hosts in order to lose quorum
	$1 swarm host-1 host-3

	# Simulate losing Control Plane/Etcd on one host
	$1 etcd host-1

	# Simulate losing Control Plane/Etcd on two hosts in order to lose quorum
	$1 etcd host-1 host-3

	# Simulate full loss of one host
	$1 full host-1

	# Simulate full loss of two hosts to lose quorum
	$1 full host-1 host-3

	# Reset the fixture back to its initial state
	$1 reset

	# Remember to include the fixture variant if you're using a non-default one
	FIXTURE_VARIANT=small $1 reset
EOF
}

main() {
	case $1 in
		swarm)
			simulate_swarm_node_loss ${@:2}
			;;
		etcd)
			simulate_etcd_node_loss ${@:2}
			;;
		full)
			simulate_full_node_loss ${@:2}
			;;
		reset)
			reset
			;;
		--help|-h)
			usage $0
			;;
		*) 
			usage $0
			exit 1
			;;
	esac
}

main $@
