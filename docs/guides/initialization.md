# Initializing a Control Plane cluster

Each Control Plane server instance starts in an uninitialized state until it's added to a cluster. In a typical configuration, you will submit a request to one Control Plane instance to initialize a new cluster, then submit requests to all other instances to join them to the new cluster.

For example, the steps to initialize a three-host cluster would look like:

1. Initialize the cluster on `host-1`
2. Join `host-2` to `host-1`'s cluster
3. Join `host-3` to `host-1`'s cluster

To initialize a cluster, make a `GET` request to the `/v1/cluster/init`
endpoint. The response will contain a "join token", which can be provided to
other instances via a `POST` request to the `/v1/cluster/join` endpoint. Using
the same example above, the initialization steps would be:

1.  Initialize the cluster on `host-1`

    === "curl"

        ```sh
        curl http://host-1:3000/v1/cluster/init
        ```

    This returns a response like:

    ```json
    {
      "token": "PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
      "server_url": "http://198.19.249.2:3000"
    }
    ```

    We'll submit this to the other Control Plane server instances to join them to
    the new cluster.

2.  Join `host-2` to `host-1`'s cluster

    === "curl"

        ```sh
        curl -X POST http://host-2:3000/v1/cluster/join \
            -H 'Content-Type:application/json' \
            --data '{
                "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
                "server_url":"http://198.19.249.2:3000"
            }'
        ```

    This will return a `204` response on success.

3.  Join `host-3` to `host-1`'s cluster

    === "curl"

        ```sh
        curl -X POST http://host-3:3000/v1/cluster/join \
            -H 'Content-Type:application/json' \
            --data '{
                "token":"PGEDGE-0c470f2eac35bb25135654a8dd9c812fc4aca4be8c8e34483c0e279ab79a7d30-907336deda459ebc79079babf08036fc",
                "server_url":"http://198.19.249.2:3000"
            }'
        ```

    The "join token" can also be fetched from any host in the cluster with a `GET`
    request the `/v1/cluster/join-token` endpoint:

    === "curl"

        ```sh
        curl http://host-1:3000/v1/cluster/join-token
        ```

    After initializing the cluster, you can submit requests to any host in the
    cluster.
