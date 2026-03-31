# Tool for capturing resource schemas

This module is a CLI tool for capturing resource schemas at a particular Git
reference to use in our state migrations. We persist a copy of the schema rather
than import it so that we can have multiple versions of the same resource schema
and so that we can continue to evolve our resource schemas without disrupting
old migrations.

To use this tool:

```sh
# create a new directory named after the new resource state version
mkdir -p server/internal/resource/migrations/schemas/vN_N_N

# example:
mkdir -p server/internal/resource/migrations/schemas/v1_0_0

# schematool can capture types from one package at a time, such as
# 'server/internal/database'. Run schematool once for each package you want to
# capture from a particular git reference and redirect the output to a file in
# the directory you created above.
go run -C ./server/internal/resource/migrations/schematool . \
    -repo '../../../../..' \
    -package vN_N_N \
    <git ref> \
    <package path for types> \
    <type names> \
> ./server/internal/resource/migrations/schemas/vN_N_N/<unique filename>.go

# example:
go run -C ./server/internal/resource/migrations/schematool . \
    -repo '../../../../..' \
    -package v1_0_0 \
    feat/PLAT-417/postgres-database-resource \
    server/internal/database \
    InstanceResource \
    LagTrackerCommitTimestampResource \
    PostgresDatabaseResource \
    ReplicationSlotAdvanceFromCTSResource \
    ReplicationSlotCreateResource \
    ReplicationSlotResource \
    SubscriptionResource \
    SyncEventResource \
    WaitForSyncEventResource \
> ./server/internal/resource/migrations/schemas/v1_0_0/database.go
```

See [1.0.0.go](../1_0_0.go) for an example of how to use the captured types to
write a migration.
