# Read replicas

As mentioned in the [Concepts](#concepts) section, a node can consist of one or
more instances, where one instance serves as a primary and the others act as
read replicas. The Control Plane creates one instance for each host ID In the
`host_ids` array for each node. So, to add read replicas for a node, specify
which hosts to deploy them on via the `host_ids` array. This example "create
database" request demonstrates a database with read replicas. Each host in this
example cluster is named after the AWS region where it resides:

```sh
curl -X POST http://host-3:3000/v1/databases \
    -H 'Content-Type:application/json' \
    --data '{
        "id": "example",
        "spec": {
            "database_name": "example",
            "database_users": [
                {
                    "username": "admin",
                    "password": "password",
                    "db_owner": true,
                    "attributes": ["SUPERUSER", "LOGIN"]
                }
            ],
            "port": 5432,
            "postgresql_conf": {
                "max_connections": 5000
            },
            "nodes": [
                { "name": "n1", "host_ids": ["us-east-1a", "us-east-1c"] },
                { "name": "n2", "host_ids": ["eu-central-1a", "eu-central-1b"] },
                { "name": "n3", "host_ids": ["ap-south-2a", "ap-south-2c"] }
            ]
        }
    }'
```

The primary instance for each node is automatically chosen by
[Patroni](https://patroni.readthedocs.io).  You can identify the primary
instance for each node by submitting a `GET` request to the
`/v1/databases/{database_id}` endpoint and inspecting the `role` and `node`
fields of each instance in the `instances` field of the response:

```sh
curl http://us-east-1a:3000/v1/databases/example
```

See [High availability client
connections](#high-availability-client-connections) below for ways to connect to
the read replicas in a high-availability use case.