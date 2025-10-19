# Connecting to a database

Once the database is available, you make a `GET` request to the
`/v1/databases/{database_id}` endpoint to get information about all of the
instances that the Control Plane created.

```sh
curl http://localhost:3000/v1/databases/example
```

The `instances` field in the response contains details about each instance, and
the `connection_info` field of each instance object contains connection
information for that specific instance.

> [!WARNING]
> If you're running the Control Plane with Docker Desktop on MacOS or Windows,
> the IP address in the `ip_address` field will be unreachable from your host
> machine. Use `localhost` instead when connecting to the instance.

> [!NOTE]
> If you have not exposed your database to outside connections, for example, by
> omitting the `port` field in your database specification, the
> `connection_info` field will be omitted in this API response.

## High-availability client connections

If your application requires high availability, we recommend using a client or
driver that supports multiple hosts. The ability to set multiple hosts is a
common feature supported by `libpq` (and any drivers or clients that use it), as
well as many drivers that do not use `libpq`, such as the [JDBC driver for
Java](https://jdbc.postgresql.org/), [`pgx` for
Go](https://github.com/jackc/pgx), and [`postgres.js` for
JavaScript](https://github.com/porsager/postgres). You can find a list of
open-source drivers by language on [the PostgreSQL
wiki](https://wiki.postgresql.org/wiki/List_of_drivers).

To use this feature, include a comma-separated list of hosts in your connection
string. For example:

```
host=host-1,host-2,host-3 port=5432,6432 user=admin password=password dbname=example
```

If the port for each database instance is the same, you can specify one port to
use for all hosts, like in this `psql` example:

```
PGPASSWORD=password psql 'host=host-1,host-2,host-3 port=5432 user=admin dbname=example'
```

By default, the driver will attempt to connect to hosts in the order they're
specified. Consider the latency between each host and your client when you order
the hosts in the connection string. Depending on your use case, it's also good
practice to set a maximum lifetime on your database connections. This way, your
client can return to the lowest-latency host following a failover and recovery.
The way that you set connection lifetime will differ between drivers and
languages.

If your database includes read replicas, you can include the
`target_session_attrs` in your connection string to only consider primary
instances or to only consider read replicas. Similar to multiple hosts, this
feature is supported by `libpq` and many other open-source drivers and clients.

This connection string uses the hosts from the [read replicas](#read-replicas)
example above to connect to the closest primary instance only:

```
host=us-east-1a,us-east-1c,u-central-1a,eu-central-1b,ap-south-2a,ap-south-2c port=5432 user=admin password=password dbname=example target_session_attrs=read-write
```

This connection string only considers connections to the read replicas:

```
host=us-east-1a,us-east-1c,u-central-1a,eu-central-1b,ap-south-2a,ap-south-2c port=5432 user=admin password=password dbname=example target_session_attrs=read-only
```

See [the PostgreSQL
documentation](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-PARAMKEYWORDS)
for detailed descriptions of all connection parameters and their possible
values.