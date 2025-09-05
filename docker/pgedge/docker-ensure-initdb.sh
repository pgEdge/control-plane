#!/usr/bin/env bash

# Copyright (c) 2025 pgEdge, Inc.
# Copyright (c) 2014, Docker PostgreSQL Authors
#
# Permission is hereby granted, free of charge, to any person
# obtaining a copy of this software and associated documentation
# files (the "Software"), to deal in the Software without
# restriction, including without limitation the rights to use,
# copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the
# Software is furnished to do so, subject to the following
# conditions:
#
# The above copyright notice and this permission notice shall be
# included in all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
# EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
# OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
# NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
# HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
# WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
# FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
# OTHER DEALINGS IN THE SOFTWARE.

set -Eeuo pipefail

#
# This script is intended for three main use cases:
#
#  1. (most importantly) as an example of how to use "docker-entrypoint.sh" to extend/reuse the initialization behavior
#
#  2. ("docker-ensure-initdb.sh") as a Kubernetes "init container" to ensure the provided database directory is initialized; see also "startup probes" for an alternative solution
#       (no-op if database is already initialized)
#
#  3. ("docker-enforce-initdb.sh") as part of CI to ensure the database is fully initialized before use
#       (error if database is already initialized)
#

source /usr/local/bin/docker-entrypoint.sh

# arguments to this script are assumed to be arguments to the "postgres" server (same as "docker-entrypoint.sh"), and most "docker-entrypoint.sh" functions assume "postgres" is the first argument (see "_main" over there)
if [ "$#" -eq 0 ] || [ "$1" != 'postgres' ]; then
	set -- postgres "$@"
fi

# see also "_main" in "docker-entrypoint.sh"

docker_setup_env
# setup data directories and permissions (when run as root)
docker_create_db_directories
if [ "$(id -u)" = '0' ]; then
	# then restart script as postgres user
	echo "Error: Running as root is not supported. Please run this script as the 'postgres' user."
	exit 1
fi

# only run initialization on an empty data directory
if [ -z "$DATABASE_ALREADY_EXISTS" ]; then
	docker_verify_minimum_env
	docker_error_old_databases

	# check dir permissions to reduce likelihood of half-initialized database
	ls /docker-entrypoint-initdb.d/ > /dev/null

	docker_init_database_dir
	pg_setup_hba_conf "$@"

	# PGPASSWORD is required for psql when authentication is required for 'local' connections via pg_hba.conf and is otherwise harmless
	# e.g. when '--auth=md5' or '--auth-local=md5' is used in POSTGRES_INITDB_ARGS
	export PGPASSWORD="${PGPASSWORD:-$POSTGRES_PASSWORD}"
	docker_temp_server_start "$@"

	docker_setup_db
	docker_process_init_files /docker-entrypoint-initdb.d/*

	docker_temp_server_stop
	unset PGPASSWORD
else
	self="$(basename "$0")"
	case "$self" in
		docker-ensure-initdb.sh)
			echo >&2 "$self: note: database already initialized in '$PGDATA'!"
			exit 0
			;;

		docker-enforce-initdb.sh)
			echo >&2 "$self: error: (unexpected) database found in '$PGDATA'!"
			exit 1
			;;

		*)
			echo >&2 "$self: error: unknown file name: $self"
			exit 99
			;;
	esac
fi
