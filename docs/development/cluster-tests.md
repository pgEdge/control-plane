The `clustertest` package is a lightweight testing framework for validating cluster operations and host management in the pgEdge Control Plane.

# Framework Overview

These tests exercise the cluster lifecycle functions, such as cluster initialization and adding or removing hosts. Unlike the E2E tests, which execute against existing control plane instances, these tests create temporary control plane instances using [testcontainers-go](https://golang.testcontainers.org/).

# Prerequisites

Before running the tests, make sure you've started the local registry and initialized the `buildx` builder:

```sh
make start-local-registry
make buildx-init
```

# Running Tests

To run all the tests:

```sh
make test-cluster
```

To skip the image build before the tests:

```sh
make test-cluster CLUSTER_TEST_SKIP_IMAGE_BUILD=1
```

To run a specific test:

```sh
make test-cluster CLUSTER_TEST_RUN=TestRemove
```

To limit the parallelism of the tests:

```sh
make test-cluster CLUSTER_TEST_PARALLEL=4
```

To test a different image tag:

```sh
make test-cluster CLUSTER_TEST_IMAGE_TAG=ghcr.io/pgedge/control-plane:v0.5.0
```

To skip cleanup:

```sh
make test-cluster CLUSTER_TEST_SKIP_CLEANUP=1
```

To use an alternate data directory:

```sh
make test-cluster CLUSTER_TEST_DATA_DIR=/tmp
```