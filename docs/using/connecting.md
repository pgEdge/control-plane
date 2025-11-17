# Connecting to a Database

Once the database is available, you make a `GET` request to the
`/v1/databases/{database_id}` endpoint to get information about all of the
instances that the Control Plane created.

=== "curl"

    ```sh
    curl http://localhost:3000/v1/databases/example
    ```

The `instances` field in the response contains details about each instance, and
the `connection_info` field of each instance object contains connection
information for that specific instance.

!!! warning

    If you're running the Control Plane with Docker Desktop on MacOS or Windows,
    the IP address in the `ip_address` field will be unreachable from your host
    machine. Use `localhost` instead when connecting to the instance.

!!! warning

    If you have not exposed your database to outside connections, for example, by omitting the `port` field in your database specification, the
    `connection_info` field will be omitted in this API response.

