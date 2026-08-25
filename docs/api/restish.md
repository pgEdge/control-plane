# Using Restish as a CLI

The pgEdge Control Plane makes use of a tool called [Restish](https://rest.sh) to get a CLI-like experience against the Control Plane's HTTP API. Restish is a generic, open-source REST client that turns any [OpenAPI](openapi.md)-described API into a set of generated commands, complete with shell completion and readable output.

Jump to what you're trying to do:

- **[Quickstart](#quickstart)** — install Restish and run your first command against a cluster.
- **[Managing Multiple Environments](#managing-multiple-environments)** — one cluster isn't enough, or you want a persistent setup.
- **[Managing Database Configuration as Files](#managing-database-configuration-as-files)** — commit database specs to source control instead of typing JSON inline.

## Quickstart

If you already have a Control Plane cluster running (see the
[installation quickstart](../installation/quickstart.md) if not), you can
be running commands against it in three steps:

First, install Restish:

```sh
brew install restish
```

Then connect Restish to your cluster:

!!! warning

    Only connect this way to clusters and databases you're okay with experimenting on. See [Managing Multiple Environments](#managing-multiple-environments)
    before connecting Restish to anything production.


```sh
restish api connect pgedge http://localhost:3000
```

Then run your first command:

```sh
restish pgedge list-databases
```

That's enough to start experimenting — `restish pgedge --help` lists every
generated command, and once you have a database config file (see
[Managing Database Configuration as Files](#managing-database-configuration-as-files)
below), `restish pgedge create-database < your-file.json` creates one.

See the [Restish install guide](https://rest.sh/docs/getting-started/install/)
for other install options, including release archives, containers, and
building from source.

## Managing Multiple Environments

Restish doesn't enforce any naming convention for the APIs you connect to.
We recommend using Restish's **profiles** feature: one API registration, `pgedge`, holds a profile per environment, and each profile can override the base URL (and, if you need it later, auth or other per-environment request details).

**Use descriptive cluster names.** Every cluster has its own durable `id`,
set at initialization and returned by `get-cluster`. `init-cluster` takes
an optional `cluster_id` query parameter. Setting it to something descriptive will allow you to keep track of multiple different clusters.

```sh
curl "http://host1.internal:3000/v1/cluster/init?cluster_id=production"
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

### Persisting Connections in a Project Config

Register `pgedge` once, with a profile per environment, in a `.restish.json`
file at the root of your infrastructure repo, instead of re-running
`restish api connect` by hand every time you check it out. Restish
discovers this file by walking up from your current directory:

```jsonc
{
  "apis": {
    "pgedge": {
      "base_url": "http://host1.internal:3000",
      "profiles": {
        "default": {},
        "staging": {
          "base_url": "http://host1.staging.internal:3000"
        },
        "production": {
          "base_url": "https://host1.prod.internal:3000"
        }
      }
    }
  }
}
```

The top-level `base_url` is what the `default` profile falls back to — the
same safe-fallback rule described above, expressed as config instead of
prose. `staging` and `production` each override it, and only take effect
when you pass `-p`.

Commit this file, then run the following once per checkout:

```sh
restish config trust
```

Restish records that trust decision outside the repo, keyed to the file's
contents, so your connections and profiles are ready every time you check
out the repo. If working with a team, everyone ends up with the same
setup without manually running `restish api connect` or keeping a personal
copy in sync.

!!! note

    A project config only honors the `apis` and `theme` keys — nothing
    else. Keep it secret-free: the Control Plane's dev and local endpoints
    don't need auth, so these profiles can stay limited to `base_url` as
    shown above. If a cluster you connect to does require auth, reference
    the value as `env:NAME` rather than committing it literally.

If a cluster has [mTLS enabled](../installation/mtls.md), add the CA and
client certificate paths to that profile. These are file paths, not the
credentials themselves, so — unlike a token or password — they're fine to
commit as long as everyone using the file also has the actual `ca.crt`,
`client.crt`, and `client.key` in place locally:

```jsonc
"production": {
  "base_url": "https://host1.prod.internal:3000",
  "ca_cert": "/opt/pgedge/control-plane/ca.crt",
  "client_cert": "/opt/pgedge/control-plane/client.crt",
  "client_key": "/opt/pgedge/control-plane/client.key"
}
```

Not every connection belongs in that shared file, though. Anything that's
 per-machine (eg. a Lima VM IP that's different for every developer, a personal sandbox) should stay out of source control. Restish also
won't let you add a profile to an API that came from trusted project
config, so register a personal one under its own name instead, which
writes to your own local Restish config:

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

Either way you connect something, the same commands work afterward:

```sh
restish api list                             # every connection you've configured
restish api inspect pgedge-sandbox           # the URL, profiles, and spec Restish resolved
restish api remove pgedge-sandbox            # disconnect (personal connections only)
restish pgedge-sandbox --help                # every generated command
restish pgedge-sandbox list-databases --help # options for one command
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
umask 077
tmpfile=$(mktemp)
trap 'rm -f "$tmpfile"' EXIT
cat > "$tmpfile" <<'EOF'
{
    "id": "example",
    "spec": {
        "database_name": "example",
        "database_users": [
            {
                "username": "admin",
                "password": "changeme",
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
restish pgedge create-database < "$tmpfile"
```

`umask 077` keeps the file unreadable by anyone else on the machine while
it exists, `mktemp` avoids a predictable filename, and the `trap` removes
it as soon as this shell exits — including if `create-database` itself
fails — rather than relying on a final `rm` that a failure or an
interrupted copy-paste could skip.

Update the same database by editing `databases/example.json` and
re-applying it against the `update-database` command. No password is
needed, since it's omitted from the request entirely:

```sh
restish pgedge update-database example < databases/example.json
```

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

The one field in these files that doesn't belong in source control is
`database_users[].password` (and, if you're configuring backups,
credentials like `s3_key_secret`). The Control Plane's update endpoint is
built to make this easy: **secret fields are only required the first time
you create a database. On every `update-database` call after that, you can
leave them out entirely** — the Control Plane keeps whatever value is
already stored unless you explicitly send a new one. See
[Updating a Database](../using/update-db.md) for the full behavior.

In practice, that means the default workflow is the one shown above: keep
`databases/example.json` secret-free from the start, and pass real secret
values only from a separate, uncommitted file for the one `create-database`
call that needs them — a file created with a restrictive `umask`, an
unpredictable `mktemp` path, and an `EXIT` trap so it's removed even if
the command fails, rather than editing secrets out of the committed file
after the fact. From then on, `update-database` runs against the
secret-free file as-is. If you need to rotate a password, apply it the
same way: the same `umask`/`mktemp`/`trap` pattern, with the new value,
passed once.

This keeps `databases/example.json` safe to read, diff, and share at any
point — it's never the file that held the credential, so there's no window
where committing it (or `git add -A`, or a stray backup) could leak one.
