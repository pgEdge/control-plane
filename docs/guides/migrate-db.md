# Migrating a Database

You can migrate data from another PostgreSQL database into your pgEdge Control Plane distributed database.

## Using pg_dump and pg_restore

The following procedure serves as a basic migration overview using the `pg_dumpall`, `pg_dump`, and `pg_restore` commands. You will need to tune them for your specific migration scenario.

1. Export and restore global objects (roles, tablespaces, etc.):

```shell
PGPASSWORD=<source_pw> pg_dumpall -g -U <superuser> -h <source_host> -p <source_port> \
  | PGPASSWORD=<dest_pw> psql -U <superuser> -h <dest_host> -p <dest_port>
```

2. Create the destination database:

```shell
PGPASSWORD=<dest_pw> createdb -U <superuser> -h <dest_host> -p <dest_port> <dbname>
```

3. Stream the source data to the destination database:

```shell
PGPASSWORD=<source_pw> pg_dump -U <superuser> -h <source_host> -p <source_port> -Fc <dbname> \
  | PGPASSWORD=<dest_pw> pg_restore -U <superuser> -h <dest_host> -p <dest_port> -d <dbname>
```

**Tip:** You can migrate data into a single-instance pgEdge Control Plane managed database and then scale up additional nodes once you have validated that the restore has completed successfully.

## Other Migration Strategies

Additional migration strategies are under development and will be documented in future releases. Potential approaches include:

* Restore to a single-instance cluster, then scale up and let Spock replicate.
* Logical replication between source and destination (requires investigation of whether Spock can be paused and resumed).