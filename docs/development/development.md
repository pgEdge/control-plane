# Developing the pgEdge Control Plane

- [Developing the pgEdge Control Plane](#developing-the-pgedge-control-plane)
  - [Running the Control Plane locally](#running-the-control-plane-locally)
  - [Pull requests](#pull-requests)
  - [Building pre-release Control Plane images](#building-pre-release-control-plane-images)
  - [Release process](#release-process)

## Running the Control Plane locally

There are two supported methods to run the Control Plane server in a local
development environment:

- [With Docker Compose](./running-locally-docker.md) (Recommended)
- [With VMs with Vagrant](./running-locally-vms.md)

## Pull requests

Your pull requests should include a changelog entry if they contain a
user-facing change from the previous release. Examples of user-facing changes
include:

- Features
- Bug fixes
- Security-related dependency updates
- User documentation updates

If your change affects something that wasn't in the previous release, for
example you've fixed a bug that was added after the last release, you can omit
the changelog entry. Likewise, you can also omit the changelog entry if your PR
doesn't contain any user-facing changes.

To create a new changelog entry, run:

```
make changelog-entry
```

And follow the interactive prompts. This adds your changelog entry to a new file
under `changes/unreleased`. We'll automatically combine these files to produce
the release changelog in the release process described below.

## Building pre-release Control Plane images

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

## Release process

We use [Semantic Versions](https://semver.org/) to identify Control Plane
releases, and we use a partially-automated process to tag and publish new
releases. To initiate a new release, run **one** of the following:

```sh
# If this is a "patch" release
make patch-release

# If this is a "minor" release
make minor-release

# If this is a "major" release
make major-release
```

This does the following:

- Creates a release changelog for the new version
- Creates a release branch named `release/<version>`
- Stages the Changelog

You'll be shown the changes and prompted to accept them. If you accept the
changes, the Make recipe will then:

- Create a commit
- Push the release branch to the origin
- Create and push release-candidate tag, e.g. `v1.0.0-rc.1`
- Print out a link to open a PR for the release branch

The new `v1.0.0-rc.1` tag will trigger a release build in CircleCI. The release
build will:

- Create a new GitHub release with:
  -  Platform-specific binaries
  -  An SBOM
  -  Checksums for the artifacts
  -  The release Changelog
- Build and publish Docker images to `ghcr.io/pgedge/control-plane`

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
