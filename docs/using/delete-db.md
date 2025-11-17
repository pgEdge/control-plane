# Deleting a Database

When you no longer need a database, you can delete it by submitting a `DELETE`
request to the `/v1/databases/{database_id}` endpoint:

=== "curl"

    ```sh
    curl -X DELETE http://host-3:3000/v1/databases/example
    ```

Deletes are asynchronous, so the response will contain a task that you can use
to track the progress of the delete.

When deleting a database, all database data will be removed from existing hosts during the deletion process. You should take care to ensure your [data is backed up](./backup-restore.md) prior to performing a delete.
