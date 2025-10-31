# Migrating a Database

You can migrate data from another PostgreSQL database into your pgEdge Control Plane distributed database.

!!! tip
    Consider the following before migration:

    - pgEdge Control Plane databases support managing only a single database within each PostgreSQL instance.
    - You must declaratively add any roles defined in your source database to the database spec passed to `create-database`.
    - pgEdge Control Plane supports only extensions bundled in the [standard images](https://github.com/pgEdge/postgres-images).

## Using pg_dump and pg_restore

The following procedure provides a basic migration overview using the `pg_dumpall`, `pg_dump`, and `pg_restore` commands. For this example, assume the source database server contains a single database named `myapp` and two user roles: `admin` and `app_role`. First, configure a new single-instance Control Plane database with the correct database name and user roles. Then use `pg_dumpall`, `pg_dump`, and `pg_restore` to migrate data and configuration from the source database server into the Control Plane database. Finally, scale up the new database with additional nodes and validate replication. For this example, the source database has been preloaded with the Northwinds sample dataset.

1. Create a new Control Plane database running a single instance of PostgreSQL:
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
                       "username": "app_role",
                       "password": "password",
                       "db_owner": false,
                       "attributes": ["LOGIN"]
                   }
               ],
               "nodes": [
                   { "name": "n1", "host_ids": ["host-1"], "port": 5432 },
               ]
           }
       }'
```

2. Export and restore global objects (roles, tablespaces, etc.):
```shell
   PGPASSWORD=<source_pw> pg_dumpall -g -U <superuser> -h <source_host> -p <source_port> \
     | PGPASSWORD=password psql -U admin -h host-1 -p 5432 myapp
```

3. Stream the source data to the destination database:
```shell
   PGPASSWORD=<source_pw> pg_dump -U <superuser> -h <source_host> -p <source_port> -Fc myapp \
     | PGPASSWORD=password pg_restore -U admin -h host-1 -p 5432 -d myapp
```

4. Validate migration by listing all the tables in the Northwind schema:
```shell
   PGPASSWORD=password psql -h host-1 -p 5432 -U admin -d myapp -c "\dt northwind.*"
                      List of tables
     Schema   |          Name          | Type  | Owner
   -----------+------------------------+-------+-------
    northwind | categories             | table | admin
    northwind | customer_customer_demo | table | admin
    northwind | customer_demographics  | table | admin
    northwind | customers              | table | admin
    northwind | employee_territories   | table | admin
    northwind | employees              | table | admin
    northwind | order_details          | table | admin
    northwind | orders                 | table | admin
    northwind | products               | table | admin
    northwind | region                 | table | admin
    northwind | shippers               | table | admin
    northwind | suppliers              | table | admin
    northwind | territories            | table | admin
    northwind | us_states              | table | admin
   (14 rows)
```

5. Scale up the Control Plane database to three nodes:
```sh
   curl -X POST http://host-1:3000/v1/databases/migrated-db \
       -H 'Content-Type:application/json' \
       --data '{
           "id": "migrated-db",
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
                       "username": "app_role",
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

6. Validate replication is active on both new nodes:

   Node n2 on port 5433:
```shell
   PGPASSWORD=password psql -h host-2 -p 5433 -U admin -d myapp -c "\dt northwind.*"
                      List of tables
     Schema   |          Name          | Type  | Owner
   -----------+------------------------+-------+-------
    northwind | categories             | table | admin
    northwind | customer_customer_demo | table | admin
    northwind | customer_demographics  | table | admin
    northwind | customers              | table | admin
    northwind | employee_territories   | table | admin
    northwind | employees              | table | admin
    northwind | order_details          | table | admin
    northwind | orders                 | table | admin
    northwind | products               | table | admin
    northwind | region                 | table | admin
    northwind | shippers               | table | admin
    northwind | suppliers              | table | admin
    northwind | territories            | table | admin
    northwind | us_states              | table | admin
   (14 rows)
```

Node n3 on port 5434:
```shell
   PGPASSWORD=password psql -h host-3 -p 5434 -U admin -d myapp -c "\dt northwind.*"
                      List of tables
     Schema   |          Name          | Type  | Owner
   -----------+------------------------+-------+-------
    northwind | categories             | table | admin
    northwind | customer_customer_demo | table | admin
    northwind | customer_demographics  | table | admin
    northwind | customers              | table | admin
    northwind | employee_territories   | table | admin
    northwind | employees              | table | admin
    northwind | order_details          | table | admin
    northwind | orders                 | table | admin
    northwind | products               | table | admin
    northwind | region                 | table | admin
    northwind | shippers               | table | admin
    northwind | suppliers              | table | admin
    northwind | territories            | table | admin
    northwind | us_states              | table | admin
   (14 rows)
```