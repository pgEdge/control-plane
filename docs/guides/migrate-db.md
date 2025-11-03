# Migrating a Database

If you have an existing Postgres database deployed on another host, you can create a new database managed by the Control Plane and migrate your data using standard Postgres tools.

!!! tip
    Consider the following before migration:

    - We recommend managing only a single Postgres database within each Postgres instance. While it is generally possible to restore data from multiple Postgres databases into a single instance, this will prevent you from distributing your database across multiple nodes.
    - You must declaratively add any roles defined in your source database to the database spec passed to `create-database`.
    - The Control Plane supports only extensions bundled in the [standard images](https://github.com/pgEdge/postgres-images).

## Using pg_dump and pg_restore

The following steps provide a basic migration overview using the [`pg_dump`](https://www.postgresql.org/docs/current/app-pgdump.html) and [`pg_restore`](https://www.postgresql.org/docs/current/app-pgrestore.html) commands. For this example, assume the source database server contains a single database named `myapp` and two user roles: `admin` and `app`.

In this approach, we will configure a new single-instance Control Plane database with the correct database name and user roles. Next, we will use `pg_dump` and `pg_restore` to migrate data and configuration from the source database server into the Control Plane database. Finally, we will scale up the new database with additional nodes and validate replication.

For this example, the source database has been preloaded with the [Northwind sample dataset](https://downloads.pgedge.com/platform/examples/northwind/northwind.sql), with the `app` user holding ownership over all objects in the `northwind` schema.

1. Create a new Control Plane database running a single instance of Postgres:

    === "curl"

        ```sh
        curl -X POST http://host-1:3000/v1/databases \
            -H 'Content-Type:application/json' \
            --data '{
                "id": "myapp",
                "spec": {
                    "database_name": "myapp",
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
                            "db_owner": false,
                            "attributes": ["LOGIN"]
                        }
                    ],
                    "nodes": [
                        { "name": "n1", "host_ids": ["host-1"], "port": 5432 }
                    ]
                }
            }'
        ```

2. Stream the source data to the destination database:

    ```shell
    PGPASSWORD=<source_pw> pg_dump -U <superuser> -h <source_host> -p <source_port> -Fc myapp -N spock \
         | PGPASSWORD=password pg_restore -U admin -h host-1 -p 5432 -d myapp
    ```

    !!! tip

        You may need to adjust [`pg_dump`](https://www.postgresql.org/docs/current/app-pgdump.html) and [`pg_restore`](https://www.postgresql.org/docs/current/app-pgrestore.html) options, depending on your database, and any errors you receive when testing the restore.

3. Validate migration by listing all the tables in the Northwind schema:

    ```shell
       PGPASSWORD=password psql -h host-1 -p 5432 -U app -d myapp -c "\dt northwind.*"
                          List of tables
         Schema   |          Name          | Type  | Owner
       -----------+------------------------+-------+-------
        northwind | categories             | table | app
        northwind | customer_customer_demo | table | app
        northwind | customer_demographics  | table | app
        northwind | customers              | table | app
        northwind | employee_territories   | table | app
        northwind | employees              | table | app
        northwind | order_details          | table | app
        northwind | orders                 | table | app
        northwind | products               | table | app
        northwind | region                 | table | app
        northwind | shippers               | table | app
        northwind | suppliers              | table | app
        northwind | territories            | table | app
        northwind | us_states              | table | app
       (14 rows)
    ```

4. Scale up the Control Plane database to three nodes:

    !!! note

        Depending on your desired architecture, you can scale by:

        - Adding replicas for your single node.
        - Adding distributed nodes (shown below)
        - Some combination of both

        === "curl"

            ```sh
            curl -X POST http://host-1:3000/v1/databases/myapp \
                -H 'Content-Type:application/json' \
                --data '{
                    "id": "myapp",
                    "spec": {
                        "database_name": "myapp",
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
                                "db_owner": false,
                                "attributes": ["LOGIN"]
                            }
                        ],
                        "nodes": [
                            { "name": "n1", "host_ids": ["host-1"], "port": 5432 },
                            { "name": "n2", "host_ids": ["host-2"], "port": 5433 },
                            { "name": "n3", "host_ids": ["host-3"], "port": 5434 }
                        ]
                    }
                }'
            ```
    
5. Validate replication is active on both new nodes (since this example scaled two additional distributed nodes).

    Node n2 on port 5433:

    ```shell
       PGPASSWORD=password psql -h host-2 -p 5433 -U app -d myapp -c "\dt northwind.*"
                          List of tables
         Schema   |          Name          | Type  | Owner
       -----------+------------------------+-------+-------
        northwind | categories             | table | app
        northwind | customer_customer_demo | table | app
        northwind | customer_demographics  | table | app
        northwind | customers              | table | app
        northwind | employee_territories   | table | app
        northwind | employees              | table | app
        northwind | order_details          | table | app
        northwind | orders                 | table | app
        northwind | products               | table | app
        northwind | region                 | table | app
        northwind | shippers               | table | app
        northwind | suppliers              | table | app
        northwind | territories            | table | app
        northwind | us_states              | table | app
       (14 rows)
    ```

    Node n3 on port 5434:

    ```shell
       PGPASSWORD=password psql -h host-3 -p 5434 -U app -d myapp -c "\dt northwind.*"
                          List of tables
         Schema   |          Name          | Type  | Owner
       -----------+------------------------+-------+-------
        northwind | categories             | table | app
        northwind | customer_customer_demo | table | app
        northwind | customer_demographics  | table | app
        northwind | customers              | table | app
        northwind | employee_territories   | table | app
        northwind | employees              | table | app
        northwind | order_details          | table | app
        northwind | orders                 | table | app
        northwind | products               | table | app
        northwind | region                 | table | app
        northwind | shippers               | table | app
        northwind | suppliers              | table | app
        northwind | territories            | table | app
        northwind | us_states              | table | app
       (14 rows)
    ```
