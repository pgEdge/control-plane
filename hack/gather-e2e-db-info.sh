#!/usr/bin/env bash

set -o errexit
set -o pipefail

# uncomment for debugging
# set -x

test_config="$1"
database_id="$2"

usage() {
    echo "Usage: $0 <path to test_config.yaml> <database ID>"
}

if [[ -z "${test_config}" || -z "${database_id}" ]]; then
    echo "error: missing required arguments"
    usage
    exit 1
fi

if [[ ! -a "${test_config}" ]]; then
    echo "error: test config file ${test_config} not found"
    usage
    exit 1
fi

echo "gathering information for database ${database_id} using test config ${test_config}"

output_dir="/tmp/${database_id}"
host_1_ip=$(yq '.hosts.host-1.external_ip' "${test_config}")
host_1_port=$(yq '.hosts.host-1.port' "${test_config}")
host_1_ssh=$(yq '.hosts.host-1.ssh_command' "${test_config}")
base_url="http://${host_1_ip}:${host_1_port}/v1"

mkdir -p ${output_dir}

echo "listing control-plane services"
${host_1_ssh} "docker service ls --filter label=com.docker.stack.namespace=control-plane" \
    > ${output_dir}/control-plane-services.txt

echo "calling list-hosts endpoint"
curl -s "${base_url}/hosts" \
    | jq \
    > ${output_dir}/list-hosts.json

echo "calling get-database endpoint"
curl -s "${base_url}/databases/${database_id}" \
    | jq \
    > ${output_dir}/get-database.json

echo "calling get-database-tasks endpoint"
curl -s "${base_url}/databases/${database_id}/tasks?sort_order=desc" \
    | jq \
    > ${output_dir}/tasks.json

echo "calling get-database-task-log endpoint"
for task_id in $(jq -r '.tasks[].task_id' ${output_dir}/tasks.json); do
    curl -s "${base_url}/databases/${database_id}/tasks/${task_id}/log" \
        | jq \
        > ${output_dir}/task-${task_id}-log.json
done

echo "calling get-database for colocated dbs"
other_dbs=""
for other_db_id in $(curl -s "${base_url}/databases" | jq -r '.databases[].id'); do
    if [[ "${other_db_id}" == "${database_id}" ]]; then
        continue
    fi
    other_dbs+=$(curl -s "${base_url}/databases/${other_db_id}")
done
<<<"${other_dbs}" jq -s > ${output_dir}/colocated-dbs.json

db_created_at=$(jq -r '.created_at' ${output_dir}/get-database.json)

for host_id in $(curl -s "${base_url}/hosts" | jq -r '.[].id'); do
    # Not using service logs here because they hang sometimes
    host_ssh=$(yq ".hosts.${host_id}.ssh_command" "${test_config}")
    host_ip=$(yq ".hosts.${host_id}.external_ip" "${test_config}")
    host_port=$(yq ".hosts.${host_id}.port" "${test_config}")

    echo "calling version endpoint on ${host_id}"
    curl -s "http://${host_ip}:${host_port}/v1/version" \
        | jq \
        > ${output_dir}/${host_id}-version.json

    echo "getting relevant control-plane container logs from ${host_id}"
    host_container_id=$(${host_ssh} "docker ps \
        --format '{{ .ID }}' \
        --filter label=com.docker.swarm.service.name=control-plane_${host_id}")
    ${host_ssh} "docker logs ${host_container_id} \
        --since ${db_created_at} 2>&1" \
        > ${output_dir}/${host_id}-control-plane.log

    echo "getting relevant docker daemon logs from ${host_id}"
    # Convert to system local time in a format compatible with journalctl
    local_since=$(${host_ssh} "date -d '${db_created_at}' +'%Y-%m-%d %H:%M:%S'")
    ${host_ssh} "sudo journalctl -u docker.service \
        --since '${local_since}' 2>&1" \
        | sed '/NetworkDB stats/d' \
        > ${output_dir}/${host_id}-docker-daemon.log
done

echo "listing postgres services"
${host_1_ssh} "docker service ls --filter label=pgedge.database.id=${database_id}" \
    > ${output_dir}/db-services.txt

for instance_id in $(jq -r '.instances[].id' ${output_dir}/get-database.json); do
    host_id=$(jq --arg id ${instance_id} '.instances[] | select(.id == $id) | .host_id' ${output_dir}/get-database.json)
    host_ssh=$(yq ".hosts.${host_id}.ssh_command" "${test_config}")

    echo "inspecting postgres service for instance ${instance_id}"
    # The ls + xargs here prevents a non-zero exit code if the service isn't
    # running.
    ${host_ssh} "docker service ls \
        --format '{{ .ID }}' \
        --filter label=pgedge.component=postgres \
        --filter label=pgedge.instance.id=${instance_id} \
        | xargs docker service inspect" \
        > ${output_dir}/${instance_id}-docker-service.json

    echo "listing postgres containers for instance ${instance_id}"
    ${host_ssh} "docker ps -a \
        --filter label=pgedge.component=postgres \
        --filter label=pgedge.instance.id=${instance_id}" \
        > ${output_dir}/${instance_id}-docker-ps.txt

    container_ids=$(${host_ssh} "docker ps -a \
        --format '{{ .ID }}' \
        --filter label=pgedge.component=postgres \
        --filter label=pgedge.instance.id=${instance_id}" \
        | tac)

    echo "getting postgres container logs for instance ${instance_id}"
    for container_id in ${container_ids}; do
        ${host_ssh} "docker logs ${container_id} 2>&1" \
            >> ${output_dir}/${instance_id}-container.log
    done

    log_dir="/data/control-plane/instances/${instance_id}/data/pgdata/log"
    log_files=$(${host_ssh} "sudo ls -tr ${log_dir} || true")

    echo "getting postgres logs for instance ${instance_id}"
    for log_file in ${log_files}; do
        ${host_ssh} "sudo cat ${log_dir}/${log_file}" \
            >> ${output_dir}/${instance_id}-postgres.log
    done
done

tar -C /tmp -czf ${database_id}.info.tar.gz ${database_id}
rm -rf "${output_dir}"

echo "wrote output to ./${database_id}.info.tar.gz"
