# Deleting a database

When you no longer need a database, you can delete it by submitting a `DELETE`
request to the `/v1/databases/{database_id}` endpoint:

```sh
curl -X DELETE http://host-3:3000/v1/databases/example
```

Deletes are asynchronous, so the response will contain a task that you can use
to track the progress of the delete.