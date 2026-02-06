# Development Process

## Running the Control Plane Locally

We use Docker Compose to run the control plane for development. Please see the
["Running the Control Plane locally"](./running-locally.md)
document for the full set of instructions.

## Pull Requests

Your pull requests should include a changelog entry if they contain a
user-facing change from the previous release. Examples of user-facing changes
include:

- features
- bug fixes
- security-related dependency updates
- user documentation updates

If your change affects something that wasn't in the previous release (for
example you've fixed a bug that was added after the last release), you can omit
the changelog entry. Likewise, you can also omit the changelog entry if your PR
doesn't contain any user-facing changes.

To create a new changelog entry, run:

```
make changelog-entry
```

And follow the interactive prompts. This adds your changelog entry to a new file
under `changes/unreleased`, which you need to add/push to the git repo. We'll 
automatically combine these files to produce the release changelog in the release 
process described below.

## Building Pre-release Control Plane Images

Make sure your local registry is running with:

```sh
make start-local-registry
```

Then create a pre-release build of the Control Plane server:

```sh
make goreleaser-build
```

Finally, build the Control Plane images and publish them to your local registry:

```sh
make control-plane-images
```

You can now pull these images from `127.0.0.1:5000/control-plane`.

## Release Process

We use [*semantic versions*](https://semver.org/) to identify Control Plane
releases, and a partially-automated process to tag and publish new
releases.

### Before You Create a Release

Make sure that there is a changelog entry for every user-facing release by
examining merged pull requests since the last release. It's easy to do this with
the [GitHub CLI](https://github.com/cli/cli):

```sh
# Get the timestamp of the last release
timestamp=$(gh release list \
  --limit 1 \
  --json publishedAt \
  --jq ".[0].publishedAt | fromdateiso8601")

# Alternatively, get the timestamp for a specific release
timestamp=$(gh release view 'v0.5.0' \
  --json publishedAt \
  --jq ".publishedAt | fromdateiso8601")

# Make sure this was successful
echo $timestamp

# Output JSON-formatted list of PRs
 GH_PAGER='' gh pr list \
  --state merged \
  --json author,number,mergeCommit,mergedAt,url,title \
  --limit 999 | \
  jq -c --argjson timestamp "${timestamp}" '.[] |
      select (.mergedAt | fromdateiso8601 > $timestamp) |
      { title, author: .author.login, url }'
```

Use the guidelines from the [Pull Requests section](#pull-requests) section to
determine which PRs should have included a changelog entry and check that one
was either included in the PR or otherwise added to the `changes/unreleased`
directory. If you find a missing entry, use `make changelog-entry` to create a
new entry before running the release process.

> [!TIP]
> If you've added a lot of new entries, it's a good idea to backup the
> `changes/unreleased` directory by copying it to another location before
> starting the release process. This can save time if you need to restart the
> release process.

### Running the Release Process

To initiate a new release, run **one** of the following:

```sh
# If this is a "patch" release
make patch-release

# If this is a "minor" release
make minor-release

# If this is a "major" release
make major-release
```

This:

- creates a release changelog for the new version.
- creates a release branch named `release/<version>`.
- stages the Changelog.

You'll be shown the changes and prompted to accept them. If you make any changes
at this point (adding files, editing files, etc.), make sure to stage those
changes with `git add` before accepting the prompt.

> [!NOTE]
> This process will replace all occurrences of the previous version number with
> the new version number in our docs. Please pay particular attention to these
> changes to ensure that they are correct and complete. Reviewers should also
> validate these changes in the release PR.

Once you accept the prompt, the make recipe will:

- create a commit.
- push the release branch to the origin.
- create and push release-candidate tag, e.g. `v1.0.0-rc.1`.
- print out a link to open a PR for the release branch.

The new `v1.0.0-rc.1` tag will trigger a release build in CircleCI. The release
build will:

- create a new GitHub release with:
  -  platform-specific binaries
  -  an SBOM
  -  checksums for the artifacts
  -  the release Changelog
- build and publish Docker images to `ghcr.io/pgedge/control-plane`.

Since the tag includes a pre-release marker, `-rc.1`, the GitHub release will be
marked as a pre-release. At this point, it's ready for quality assurance and
testing.

If we find bugs in the release, the fixes should be PR'd or pushed into the
release branch. Then, we must create a new release candidate by creating and
pushing a new tag with an incremented `rc` number, e.g.: `v1.0.0-rc.2`.

Once we're confident that the release is ready, a reviewer must approve the
release PR, and then we can merge it.

Merging the release PR will trigger a GitHub workflow to create the release tag,
for example, `v1.0.0`. This new tag will trigger the same build process
described above for the completed release.
