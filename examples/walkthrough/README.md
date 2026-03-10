# Guided Walkthrough — Developer Reference

This directory contains an interactive walkthrough that deploys a 3-node
distributed Postgres database with active-active multi-master replication
using the Spock extension, all orchestrated by the pgEdge Control Plane.

> The end-user walkthrough is at [docs/walkthrough.md](../../docs/walkthrough.md).
> This README covers how the walkthrough is structured and how to run it.

## How Users Reach the Walkthrough

The walkthrough is self-contained — it does not clone this repository or
depend on git. There are two primary entrypoints:

1. `curl ... | bash` — a one-liner that runs `install.sh` remotely. It
   downloads the walkthrough files, checks prerequisites, and launches
   the interactive guide. Requires Docker, curl, jq, and psql.

2. GitHub Codespaces — opens a pre-configured environment with
   Docker-in-Docker and all tools pre-installed. The devcontainer runs
   `post-create.sh` during creation and opens `docs/walkthrough.md`
   with runnable code blocks via the Runme extension.

The walkthrough commands are also available in
[docs/walkthrough.md](../../docs/walkthrough.md), which is published on
[docs.pgedge.com](https://docs.pgedge.com).

If a user runs the `curl ... | bash` install inside Codespaces,
`install.sh` detects the `$CODESPACES` environment variable and exits
as a no-op — the devcontainer has already handled setup.

Both paths walk through the same progression:

1. Start the Control Plane — Docker Swarm init, pull and run the
   Control Plane container, initialize the cluster
2. Create a Distributed Database — declare a 3-node database via
   the REST API, wait for it to become available
3. Verify Multi-Master Replication — write on one node, read from
   another, confirm bidirectional replication
4. Resilience Demo — scale a node down, write while it's offline,
   bring it back, verify zero data loss

## File Overview

```
examples/walkthrough/
├── install.sh      # Curl-pipe entry point (downloads files, runs setup)
├── guide.sh        # Interactive guide (4 steps)
├── setup.sh        # Prerequisites checker
└── runner.sh       # Terminal UX framework (sourced, not executed)
```

### install.sh

Entry point for `curl ... | bash`. Downloads individual files from
GitHub (no git clone) into a self-contained `pgedge-cp-walkthrough/`
directory that mirrors the repo layout. Runs `setup.sh` for
prerequisites, then prompts the user to choose between the interactive
guide (`guide.sh`) or the markdown walkthrough (`docs/walkthrough.md`).

Environment variables:
- `WALKTHROUGH_DIR` — override output directory (default:
  `pgedge-cp-walkthrough`)
- `WALKTHROUGH_BRANCH` — override GitHub branch (default: `main`)

In Codespaces (`$CODESPACES` is set), it exits early as a no-op since
the devcontainer has already handled setup.

### setup.sh

Validates that all required tools are present and the Docker environment
is ready. Does not install anything — it reports what's missing with
platform-aware install hints and exits non-zero if prerequisites aren't
met.

Checks performed:
- Required commands: `docker`, `curl`, `jq`, `psql`, plus `lsof`
  (macOS) or `ss` (Linux) for port detection
- Docker daemon accessibility (with platform-specific diagnostics:
  Docker Desktop on macOS, systemctl on Linux, generic fallback)
- Docker Desktop host networking on macOS (checks
  `settings-store.json`)

### guide.sh

The interactive guide. Sources `runner.sh` for terminal UX, then walks
through all four steps with explanatory text, `prompt_run` commands, and
spinners for slow operations.

Key behaviors:
- Detects existing Control Plane containers and reuses them on rerun
- Removes stale (stopped) containers from previous runs automatically
- Detects available ports (falls back to alternatives if 5432-5434 are
  occupied)
- On rerun with an existing database, reads ports from the API so psql
  calls target the correct instances
- Proper error handling for cluster initialization (distinguishes
  success, already-initialized, and real failures)
- Polls for Postgres readiness and replication sync (not fixed sleeps)
- Platform-aware Docker Swarm init (`--advertise-addr` on Linux for
  multi-interface hosts, plain init on macOS)
- Idempotent — safe to re-run at any point

Environment variables:
- `CP_IMAGE` — override Control Plane image (default:
  `ghcr.io/pgedge/control-plane`)

### runner.sh

Reusable terminal UX framework, sourced by `guide.sh`. Provides:

- Brand colors — teal (`\033[38;5;30m`) and orange
  (`\033[38;5;172m`) from the pgEdge palette
- `header` — teal bordered section headers
- `show_cmd` / `prompt_run` — shows a command with orange `$` prefix,
  waits for Enter, runs it with framed output
- `prompt_continue` — simple "Press Enter to continue" gate
- `start_spinner` / `stop_spinner` — braille dot animation for
  long-running operations
- `explain` / `info` / `warn` / `error` — plain text and colored
  status messages

This file is standalone and could be reused for other interactive
guides.

## Running the Walkthrough

### Interactive Guide (`curl ... | bash`)

The primary entrypoint. Downloads the walkthrough files, checks
prerequisites, and launches the interactive guide. Requires Docker,
curl, jq, and psql.

```bash
curl -fsSL https://raw.githubusercontent.com/pgEdge/control-plane/main/examples/walkthrough/install.sh | bash
```

What happens:
1. `install.sh` downloads scripts and `docs/walkthrough.md` into
   `pgedge-cp-walkthrough/` (no git clone — individual file downloads)
2. `setup.sh` checks that Docker, curl, jq, psql, and lsof/ss are
   available and the Docker daemon is accessible
3. User chooses: run the interactive guide (`guide.sh`), or exit and
   follow `docs/walkthrough.md` manually

### GitHub Codespaces

Open a Codespace with the walkthrough devcontainer:

```
https://codespaces.new/pgEdge/control-plane?devcontainer_path=.devcontainer/walkthrough/devcontainer.json
```

The devcontainer
([`.devcontainer/walkthrough/`](../../.devcontainer/walkthrough/))
handles everything:

1. Base image — Ubuntu with Docker-in-Docker pre-configured
2. `post-create.sh` — installs jq and the pgEdge Postgres client
   (`pgedge-postgresql-client-18`) from the pgEdge apt repository,
   then runs `setup.sh` to verify prerequisites
3. `postAttachCommand` — opens `docs/walkthrough.md` in the editor
4. Runme extension — pre-installed so users can run markdown code
   blocks directly

Users can run the code blocks in `walkthrough.md` directly from the
editor, or switch to the interactive guide:
`bash examples/walkthrough/guide.sh`.

### From a cloned repo

The walkthrough is designed to work without cloning the repo, but if
you already have it cloned you can run the pieces directly:

```bash
# Run the interactive guide (calls setup.sh automatically)
bash examples/walkthrough/guide.sh

# Or run setup and guide separately
bash examples/walkthrough/setup.sh
bash examples/walkthrough/guide.sh
```
