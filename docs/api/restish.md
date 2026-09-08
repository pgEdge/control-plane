# Using Restish as a CLI

The pgEdge Control Plane can be used with a tool called [Restish](https://rest.sh) to get a CLI-like experience against the Control Plane's HTTP API. Restish is a generic, open-source REST client that turns any [OpenAPI](openapi.md)-described API into a set of generated commands, complete with shell completion and readable output.

Jump to what you're trying to do:

- **[Quickstart](#quickstart)** — install Restish and run your first command against a cluster.
- **[Managing Multiple Environments](#managing-multiple-environments)** — one cluster isn't enough, or you want a persistent setup.
- **[Managing Database Configuration as Files](#managing-database-configuration-as-files)** — commit database specs to source control instead of typing JSON inline.

## Quickstart

If you already have a Control Plane cluster running (see the
[installation quickstart](../installation/quickstart.md) if not), you can
be running commands against it in three steps:


### 1. Installation

First, install Restish [via Restish's official website](https://rest.sh/docs/getting-started/install/). Restish supports many installation methods, including Homebrew (macOS), GitHub Releases, and OCI images; select the option that most aligns with your organization's preferences and practices.

### 2. Connection

Note: 
    Only connect this way to clusters and databases you're okay with experimenting on. See [Managing Multiple Environments](#managing-multiple-environments)
    before connecting Restish to anything production.

Connect Restish to your cluster:


```sh
restish api connect pgedge http://localhost:3000
```
### 3. Verification

Then run your first command:

```sh
restish pgedge list-databases
```

That's enough to start experimenting — `restish pgedge --help` lists every
generated command, and once you have a database config file (see
[Managing Database Configuration as Files](#managing-database-configuration-as-files)
below), `restish pgedge create-database < your-file.json` creates one.

## Managing Multiple Environments

Restish doesn't enforce any naming convention for the APIs you connect to.
We recommend using Restish's **profiles** feature: one API registration, `pgedge`, holds a profile per environment, and each profile can override the base URL (and, if you need it later, auth or other per-environment request details).

**Use descriptive cluster names.** Every cluster has an **immutable** `id`,
set at initialization and returned by `get-cluster`. `init-cluster` takes
an optional `cluster_id` query parameter. Setting it to something descriptive will allow you to keep track of multiple different clusters.

```sh
restish pgedge init-cluster --cluster-id production
```

Then select that cluster with a matching profile:

```sh
restish -p production pgedge list-databases
restish -p staging pgedge list-databases
```

**Leave the `default` profile pointed at something safe.** Restish falls
back to the `default` profile whenever you don't pass `-p`/`--rsh-profile`
or set `RSH_PROFILE`, so be sure to point `default` at your local or informal cluster.

```sh
restish pgedge list-databases   # default profile: local/informal cluster
```

!!! note

    A cluster is made up of multiple hosts, each with its own host ID
    (e.g. `host-1`). Any of them can serve a request for the whole
    cluster, which is why one `base_url` per environment is enough for
    routine use — you don't need a profile per host. To target a specific
    host directly (retrying against a different one, or comparing behavior
    across hosts while debugging), give it its own profile the same way:
    `-p production-host-1`.

Add a profile per environment to the same `pgedge` registration with
`restish api set`:

```sh
restish api set pgedge 'profiles.staging.base_url: http://host1.staging.internal:3000'
restish api set pgedge 'profiles.production.base_url: https://host1.prod.internal:3000'
```

Not every connection belongs on your everyday `pgedge` registration,
though. Anything that's per-machine (eg. a Lima VM IP that's different for
every developer, a personal sandbox) is better off registered under its
own name so it doesn't collide with your regular setup:

```sh
restish api connect pgedge-sandbox http://192.168.64.3:3000
```

If a cluster uses TLS with a private CA, pass `--rsh-ca-cert` with the CA
file so Restish trusts it; if discovery also fails for a connection set up
this way, add `--spec` with an explicit URL or local file:

```sh
restish api connect pgedge-sandbox https://192.168.64.3:3000 \
    --rsh-ca-cert ./ca.crt \
    --spec https://192.168.64.3:3000/v1/openapi.json
```

If the cluster also requires a client certificate for mTLS, see
[Connecting Over mTLS](#connecting-over-mtls).

Either way you connect something, the same commands work afterward:

```sh
restish api list                             # every connection you've configured
restish api inspect pgedge-sandbox           # the URL, profiles, and spec Restish resolved
restish api remove pgedge-sandbox            # disconnect (personal connections only)
restish pgedge-sandbox --help                # every generated command
restish pgedge-sandbox list-databases --help # options for one command
```

Restish also supports registering connections in a `.restish.json` project
config file so a whole team shares the same setup automatically — most
See Restish's own docs on
[project configuration](https://rest.sh/docs/reference/config/).

### Connecting Over mTLS

If a cluster has [mTLS enabled](../installation/mtls.md), pass the CA
certificate plus a client certificate and key when you connect:

```sh
restish api connect pgedge-sandbox https://192.168.64.3:3000 \
    --rsh-ca-cert ./ca.crt \
    --rsh-client-cert ./client.crt \
    --rsh-client-key ./client.key
```

For a shared registration like `pgedge`, set the same paths per profile
with `restish api set`:

```sh
restish api set pgedge \
    'profiles.production.ca_cert: /opt/pgedge/control-plane/ca.crt' \
    'profiles.production.client_cert: /opt/pgedge/control-plane/client.crt' \
    'profiles.production.client_key: /opt/pgedge/control-plane/client.key'
```

## Managing Database Configuration as Files

Keep one file per database, and use it as the source of truth for that
database's configuration:

```sh
mkdir -p databases
cat > databases/example.json <<'EOF'
{
    "id": "example",
    "spec": {
        "database_name": "example",
        "database_users": [
            {
                "username": "admin",
                "db_owner": true,
                "attributes": ["SUPERUSER", "LOGIN"]
            }
        ],
        "port": 5432,
        "nodes": [
            { "name": "n1", "host_ids": ["host-1"] },
            { "name": "n2", "host_ids": ["host-2"] },
            { "name": "n3", "host_ids": ["host-3"] }
        ]
    }
}
EOF
```

`databases/example.json` never contains a password, so it's safe to commit
right away. Creating a database still needs a real password the first
time, though, so pass that from a separate file you don't commit instead
of adding it to `databases/example.json`:

```sh
(
  set -e
  umask 077
  tmpfile=$(mktemp)
  trap 'rm -f "$tmpfile"' EXIT
  read -rsp "Password: " DB_PASS; echo
  jq --arg pw "$DB_PASS" '.spec.database_users[0].password = $pw' \
    databases/example.json > "$tmpfile"
  restish pgedge create-database < "$tmpfile"
)
```

`read -rsp` prompts for the password without echo, so it never appears in
your terminal output or shell history. `jq` receives the value via
`--arg` and injects it into `databases/example.json` at runtime, so the
password never appears in the command text itself. `umask 077` keeps the
tmpfile unreadable by anyone else on the machine while it exists, and the
subshell `(...)` limits the `trap`'s scope: when `create-database` returns
(or fails), the subshell exits and the trap fires immediately, removing the
file before control returns to your interactive shell.

Update the same database by editing `databases/example.json` and
re-applying it against the `update-database` command. No password is
needed, since it's omitted from the request entirely:

```sh
restish pgedge update-database example < databases/example.json
```

!!! note

    Restish retries network errors and transient server errors (`408`,
    `429`, `500`, `502`, `503`, `504`) automatically, but not `create-database`,
    `update-database`, or `delete-database` themselves — POST/PUT/PATCH/DELETE
    requests are only retried if you explicitly pass `--rsh-retry-unsafe`,
    which prints a warning when used. Leave that flag off for database
    operations: retrying a request that already partially succeeded on the
    server can double-process it.

To apply the same file to a specific environment instead of your default
cluster, add the profile you set up in
[Managing Multiple Environments](#managing-multiple-environments):

```sh
restish -p staging pgedge update-database example < databases/example.json
restish -p production pgedge update-database example < databases/example.json
```

This gives you a directory of database configuration files you can commit
to source control, diff, review in a pull request, and re-apply — the same
workflow you'd use for any other infrastructure-as-code.

### Handling Secrets

Secret fields, such as `database_users[].password` or `s3_key_secret`,
should be excluded from any files committed to source control. The
Control Plane's update endpoint is built to make this easy: **secret
fields are only required the first time
you create a database. On every `update-database` call after that, you can
leave them out entirely** — the Control Plane keeps whatever value is
already stored unless you explicitly send a new one. See
[Updating a Database](../using/update-db.md) for the full behavior.

In practice, that means the default workflow is the one shown above: keep
`databases/example.json` secret-free from the start, and pass real secret
values only from a separate, uncommitted tmpfile for the one
`create-database` call that needs them — prompted interactively with echo
disabled (so the value never enters shell history), injected via `jq` at
runtime, and written to a file created with a restrictive `umask` inside a
subshell so it's removed as soon as `create-database` returns (even if it
fails). From then on, `update-database` runs against the secret-free file
as-is. If you need to rotate a password, apply it the same way: `read -rsp`
for the new value, `jq` to inject it, passed once.

This keeps `databases/example.json` safe to read, diff, and share at any
point — it's never the file that held the credential, so there's no window
where committing it (or `git add -A`, or a stray backup) could leak one.

!!! tip

    The Control Plane excludes every secret field from its responses, you can skip manual redaction entirely: create the database from a one-off request that includes all of its secrets, then pull the sanitized spec back into your file:

    ```sh
    restish pgedge get-database example | jq '{ id, spec }' > databases/example.json
    ```
