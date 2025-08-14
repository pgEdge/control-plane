# Overridable vars
# Sets the image repository
PGEDGE_IMAGE_REPO ?= 127.0.0.1:5000/pgedge-postgres
# When set to "1", images will be rebuilt and republished
PGEDGE_IMAGE_REPUBLISH ?= 0
# When set to "1", build and publish steps will be skipped
PGEDGE_IMAGE_DRY_RUN ?= 0
# When set to "1", build will run with the --no-cache flag
PGEDGE_IMAGE_NO_CACHE ?= 0
# When set to a postgres major.minor version, will restrict build and publish to
# that postgres version.
PGEDGE_IMAGE_ONLY_POSTGRES_VERSION ?=
# When set to a spock major.minor.patch version, will restrict build and publish
# to that spock version.
PGEDGE_IMAGE_ONLY_SPOCK_VERSION ?=
# When set to a specific architecture, e.g. "amd64" or "arm64", will restrict
# build and publish to that architecture.
# WARNING: this should only be used for testing because the resulting manifest
# will only be usable on one architecture.
PGEDGE_IMAGE_ONLY_ARCH ?=
# These builders are defined in the main Makefile. In CI, we run builds
# sequentially.
BUILDX_BUILDER=$(if $(CI),"control-plane-ci","control-plane")

.PHONY: pgedge-images
pgedge-images:
	PGEDGE_IMAGE_REPO=$(PGEDGE_IMAGE_REPO) \
	PGEDGE_IMAGE_REPUBLISH=$(PGEDGE_IMAGE_REPUBLISH) \
	PGEDGE_IMAGE_DRY_RUN=$(PGEDGE_IMAGE_DRY_RUN) \
	PGEDGE_IMAGE_NO_CACHE=$(PGEDGE_IMAGE_NO_CACHE) \
	PGEDGE_IMAGE_ONLY_POSTGRES_VERSION=$(PGEDGE_IMAGE_ONLY_POSTGRES_VERSION) \
	PGEDGE_IMAGE_ONLY_SPOCK_VERSION=$(PGEDGE_IMAGE_ONLY_SPOCK_VERSION) \
	PGEDGE_IMAGE_ONLY_ARCH=$(PGEDGE_IMAGE_ONLY_ARCH) \
	BUILDX_BUILDER=$(BUILDX_BUILDER) \
	./scripts/build_pgedge_images.py
