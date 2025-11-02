_cp_hack_dir=$(grealpath $(gdirname "${(%):-%x}"))
_cp_dir=$(grealpath "${_cp_hack_dir}/..")

####################
# helper functions #
####################

_unset-cp-aliases() {
	local aliases=$(alias \
		| grep '^cp[0-9]-\(req\|api-sync\|ssh\)=' \
		| cut -d'=' -f1)

	if [[ -n "${aliases}" ]]; then
		unalias $(echo ${aliases})
	fi
}

_update-restish-config() {
	export RESTISH_CONFIG_DIR="${_cp_hack_dir}/apis/${CP_ENV}"
	export RESTISH_CACHE_DIR="${RESTISH_CONFIG_DIR}"

	mkdir -p "${RESTISH_CONFIG_DIR}"

	<<<"$@" jq -R 'split(" ")
		| to_entries
		| [ .[]
		| { key: ("host-" + (.key+1 | tostring)), value: { base: .value } }]
		| from_entries
		| {"$schema": "https://rest.sh/schemas/apis.json"} + .' \
		> "${RESTISH_CONFIG_DIR}/apis.json"

	_unset-cp-aliases

	for ((i = 1; i <= ${#*[@]}; i++ )); do
		alias cp${i}-req="restish host-${i}"
		alias cp${i}-api-sync="restish api sync host-${i}"
	done
}

_test-config() {
	echo "${_cp_dir}/e2e/fixtures/outputs/${CP_ENV}.test_config.yaml"
}

_ssh-command() {
	local test_config="$(_test-config)"

	host_id="$1" yq -r '.hosts[env(host_id)].ssh_command' "${test_config}"
}

_use-test-config() {
	local test_config="$(_test-config)"

	if [[ ! -e "${test_config}" ]]; then
		echo "test config ${test_config} does not exist" >&2
		echo "please deploy the ${CP_ENV} test fixture before using this function" >&2
		return 1
	fi

	_update-restish-config \
		$(yq -r '.hosts[].external_ip | "http://" + . + ":3000"' "${test_config}")

	local ssh_command
	local i=1
	for host_id in $(yq -r '.hosts|to_entries|.[].key' "${test_config}"); do
		ssh_command=$(_ssh-command "${host_id}")
		alias cp${i}-ssh="${ssh_command}"

		((i++))
	done
}

_choose-instance() {
	local instance_choice=$(restish host-1 list-databases \
		| jq -c '.databases[]? | { database_id: .id } + (.instances[]?)' \
		| sk --preview 'echo {} | jq')

	if [[ -z "${instance_choice}" ]]; then
		return 1
	fi

	local instance_id=$(jq -r '.id' <<<"${instance_choice}")
	echo "using instance ${instance_id}" >&2
	echo "${instance_choice}"
}

_choose-user() {
	local user_choice=$(<<<"$1" \
		jq -c '[.spec.database_users[]?, {"username": "pgedge"}][]' \
		| sk --preview 'echo {} | jq')

	if [[ -z "${user_choice}" ]]; then
		return 1
	fi

	local username=$(jq -r '.username' <<<"${user_choice}")
	echo "using user ${username}" >&2
	echo ${username}
}

_docker-cmd() {
	local host_id="$1"
	local args=(${*[@]:2})
	local docker=("docker" ${args[@]})

	if [[ "${CP_ENV}" != "compose" ]]; then
		local test_config="$(_test-config)"
		local ssh_command=$(_ssh-command "${host_id}")

		if [[ ! -t 0 ]]; then
			docker=(${(@s: :)ssh_command} '-q' '-T' ${(qq)docker[@]})
		else
			docker=(${(@s: :)ssh_command} '-q' '-t' ${(qq)docker[@]})
		fi
	fi

	${docker[@]}
}

_instance-container-id() {
	local host_id="$1"
	local instance_id="$2"

	_docker-cmd "${host_id}" ps \
        --format '{{ .ID }}' \
        --filter "label=pgedge.instance.id=${instance_id}"
}

_psql-docker-exec() {
	local host_id="$1"
	local instance_id="$2"
	local db_user="$3"
	local db_name="$4"
	local args=(${*[@]:5})
	# The /dev/null redirect prevents ssh from consuming our stdin if we're
	# using a remote environment like lima or ec2.
	local container_id=$(_instance-container-id "${host_id}" "${instance_id}" </dev/null)
	local exec_args=("${container_id}" psql -U "${db_user}" -d "${db_name}" ${args[@]})

    # Tests if stdin is a tty
    if [[ ! -t 0 ]]; then
        _docker-cmd "${host_id}" exec -i ${exec_args[@]}
    else
        _docker-cmd "${host_id}" exec -it ${exec_args[@]}
    fi
}

# TODO: we won't need this after PLAT-220
_translate-ip() {
	local ip_in="$1"

	if [[ "${CP_ENV}" == "compose" ]]; then
		echo "${ip_in}"
		return
	fi

	local test_config="$(_test-config)"

	peer_ip="${ip_in}" yq '.hosts[] 
		| select(.peer_ip == env(peer_ip)) 
		| .external_ip' \
		"${test_config}"
}

_psql-local() {
	local instance_id="$1"
	local db_user="$2"
	local db_name="$3"
	local get_db_resp="$4"
	local args=${*[@]:5}
	local conn_info=$(<<<"${get_db_resp}" \
		jq --arg instance_id "${instance_id}" '.instances[] | select(.id == $instance_id) | .connection_info')

	if [[ -z "${conn_info}" ]]; then
		echo "instance ${instance_id} has no connection info. does it expose a port?" >&2
		return 1
	fi

	local ip_addr=$(<<<"${conn_info}" \
		jq -r '.ipv4_address')
	local port=$(<<<"${conn_info}" \
		jq -r '.port')

	# TODO: PLAT-220
	ip_addr=$(_translate-ip "${ip_addr}")

	local psql_cmd=(psql -h "${ip_addr}" -p "${port}" -U "${db_user}" -d "${db_name}" ${args[@]})

	${psql_cmd[@]}
}

###################
# shell functions #
###################

use-compose() {
	export CP_ENV=compose

	_update-restish-config \
		http://localhost:3000 \
		http://localhost:3001 \
		http://localhost:3002 \
		http://localhost:3003 \
		http://localhost:3004 \
		http://localhost:3005 \
}

use-lima() {
	export CP_ENV=lima

	_use-test-config
}

use-ec2() {
	export CP_ENV=ec2

	_use-test-config
}

_cp-psql-help() {
	cat <<EOF
$1 [-h|--help]
$1 <-i|--instance-id instance id> <-U|--username> <-m|--method docker|local> -- [...psql opts and args]

Examples:
	# By default, this command will present interactive instance and user
	# pickers and connect via 'docker exec'
	$1

	# Connect using a specific instance and user
	$1 -i storefront-n1-689qacsi -U admin

	# Connect using a locally-running psql client
	PGPASSWORD=password $1 -i storefront-n1-689qacsi -U admin -m local

	# Include a '--' separator to pass additional psql args
	$1 -i storefront-n1-689qacsi -U admin -- -c 'select 1'

	# Stdin also works
	echo 'select 1' | $1 -i storefront-n1-689qacsi -U admin
EOF
}

cp-psql() {
    local o_instance_id
	local o_username
	local o_method
    local o_help

	zparseopts -D -F -K -- \
        {i,-instance-id}:=o_instance_id \
		{m,-method}:=o_method \
		{U,-username}:=o_username \
        {h,-help}=o_help || return

	if (($#o_help)); then
        _cp-psql-help $0
        return
    fi

	local instance_id="${o_instance_id[-1]}"
	local username="${o_username[-1]}"
	local method="${o_method[-1]:-docker}"	
	local database_id

	if [[ -z "${instance_id}" ]]; then
		instance=$(_choose-instance)

		if [[ -z "${instance}" ]]; then
			return 1
		fi

		instance_id=$(<<<"${instance}" jq -r '.id')
		database_id=$(<<<"${instance}" jq -r '.database_id')
	else
		database_id=$(restish host-1 list-databases \
			| jq --arg instance_id "${instance_id}" -r '.databases[]?
				| { database_id: .id } + (.instances[]?)
				| select(.id == $instance_id)
				| .database_id')

		if [[ -z "${database_id}" ]]; then
			echo 'no database found with the given instance id' >&2
			return 1
		fi
	fi

	local get_db_resp=$(restish host-1 get-database "${database_id}")
	local db_name=$(jq <<<"${get_db_resp}" -r '.spec.database_name')	

	local host_id=$(jq <<<"${get_db_resp}" -r \
		--arg instance_id "${instance_id}" \
		'.instances[] 
		| select(.id == $instance_id)
		| .host_id')

	if [[ -z "${username}" ]]; then
		username=$(_choose-user "${get_db_resp}")
	fi
	if [[ -z "${username}" ]]; then
		return 1
	fi

	case "${method}" in
		local)
			_psql-local "${instance_id}" "${username}" "${db_name}" "${get_db_resp}" $@
			;;
		docker)
			_psql-docker-exec "${host_id}" "${instance_id}" "${username}" "${db_name}" $@
			;;
		*)
			echo "unrecognized method ${method}" >&2
			return 1
			;;
	esac
}

_cp-docker-exec-help() {
	cat <<EOF
$1 [-h|--help]
$1 <-i|--instance-id instance id> command [... command args]

Examples:
	# By default, this command will present an interactive instance picker and
	# open a bash shell in the target instance
	$1

	# Open a bash shell on a specific instance
	$1 -i storefront-n1-689qacsi

	# Open a bash shell as a specific user
	$1 -i storefront-n1-689qacsi -u root

	# Start a command with arguments
	$1 -i storefront-n1-689qacsi psql -U admin storefront -c 'select 1'

	# Also works with stdin
	echo 'select 1' | $1 -i storefront-n1-689qacsi psql -U admin storefront
EOF
}

cp-docker-exec() {
	local o_instance_id
	local o_user
    local o_help

	zparseopts -D -F -K -- \
        {i,-instance-id}:=o_instance_id \
		{u,-user}:=o_user \
        {h,-help}=o_help || return

	if (($#o_help)); then
		_cp-docker-exec-help $0
        return
    fi

	local args=($@)
	if [[ -z "${args}" ]]; then
		# run bash by default
		args=("bash")
	fi

	local instance_id="${o_instance_id[-1]}"
	local user="${o_user[-1]}"
	local host_id
	if [[ -z "${instance_id}" ]]; then
		local instance_choice=$(_choose-instance)

		if [[ -z "${instance_choice}" ]]; then
			return 1
		fi

		instance_id=$(<<<"${instance_choice}" jq -r '.id')
		host_id=$(<<<"${instance_choice}" jq -r '.host_id')
	else
		host_id=$(restish host-1 list-databases \
			| jq --arg instance_id "${instance_id}" -r '.databases[]?.instances[]?
				| select(.id == $instance_id)
				| .host_id')

		if [[ -z "${host_id}" ]]; then
			echo 'no instance found with given id' >&2
			return 1
		fi
	fi

	local container_id=$(_instance-container-id "${host_id}" "${instance_id}" </dev/null)
	local exec_args=("${container_id}" ${args[@]})

	if [[ -n "${user}" ]]; then
		exec_args=('-u' "${user}" ${exec_args[@]})
	fi

	if [[ ! -t 0 ]]; then
        _docker-cmd "${host_id}" exec -i "${exec_args[@]}"
    else
        _docker-cmd "${host_id}" exec -it "${exec_args[@]}"
    fi
}

cp-init() {
	local host_ids=$(jq -r 'keys 
		| .[] 
		| select(contains("host"))' \
		"${RESTISH_CONFIG_DIR}/apis.json")
	local host_id
	local join_token
	local resp
	local uninitialized=()

	for host_id in ${(f)host_ids}; do
		echo "checking if ${host_id} is initialized" >&2

		resp=$(restish ${host_id} --rsh-ignore-status-code get-join-token)

		if [[ $(<<<"${resp}" jq '.token') == "null" ]]; then
			uninitialized+=(${host_id})
		elif [[ -z "${join_token}" ]]; then
			join_token="${resp}"
		fi
	done

	if [[ -z "${join_token}" ]]; then
		echo "initializing cluster from ${uninitialized[1]}" >&2

		# zsh arrays are 1-indexed
		join_token=$(restish ${uninitialized[1]} init-cluster)

		# delete the first array element
		uninitialized[1]=()
	fi

	for host_id in ${uninitialized[@]}; do
		echo "joining ${host_id} to the cluster" >&2

		restish ${host_id} join-cluster "${join_token}" > /dev/null
	done
}

#########
# setup #
#########

# default to compose env
use-compose

##################
# static aliases #
##################

_host_1_data="${_cp_dir}/docker/control-plane-dev/data/host-1"
_host_1_certs="${_host_1_data}/certificates"
_host_1_cfg="${_host_1_data}/generated.config.json"

alias cp-etcdctl="etcdctl \
	--endpoints=https://localhost:2379 \
	--cacert '${_host_1_certs}/ca.crt' \
	--cert '${_host_1_certs}/etcd-user.crt' \
	--key '${_host_1_certs}/etcd-user.key' \
	--user \$(jq -r '.etcd_username' '${_host_1_cfg}') \
	--password \$(jq -r '.etcd_password' '${_host_1_cfg}')"

alias cp-docker-compose="WORKSPACE_DIR=${_cp_dir} \
	DEBUG=\${DEBUG:-0} \
	LOG_LEVEL=\${LOG_LEVEL:-info} \
	DEV_IMAGE_REPO=\${DEV_IMAGE_REPO:-ghcr.io/pgedge} \
	docker compose -f ./docker/control-plane-dev/docker-compose.yaml"
