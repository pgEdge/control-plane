include common.mk

# Overridable vars
CI ?= false
DEBUG ?= 0
LOG_LEVEL ?= info
DEV_IMAGE_REPO ?= ghcr.io/pgedge
CONTROL_PLANE_IMAGE_REPO ?= host.docker.internal:5000/control-plane
TEST_RERUN_FAILS ?= 0
E2E_FIXTURE ?=
E2E_PARALLEL ?= 8
E2E_RUN ?=
E2E_SKIP_CLEANUP ?= 0
E2E_DEBUG ?= 0
E2E_DEBUG_DIR ?=
FIXTURE_VARIANT ?= large
FIXTURE_CONTROL_PLANE_IMAGE ?=
FIXTURE_EXTRA_VARS ?=
CLUSTER_TEST_PARALLEL ?=
CLUSTER_TEST_RUN ?=
CLUSTER_TEST_SKIP_IMAGE_BUILD ?= 0
CLUSTER_TEST_SKIP_CLEANUP ?= 0
CLUSTER_TEST_IMAGE_TAG ?=
CLUSTER_TEST_DATA_DIR ?=

docker_swarm_state=$(shell docker info --format '{{.Swarm.LocalNodeState}}')
buildx_builder=$(if $(CI),"control-plane-ci","control-plane")
buildx_config=$(if $(CI),"./buildkit.ci.toml","./buildkit.toml")
docker_compose_dev=WORKSPACE_DIR=$(shell pwd) \
		DEBUG=$(DEBUG) \
		LOG_LEVEL=$(LOG_LEVEL) \
		DEV_IMAGE_REPO=$(DEV_IMAGE_REPO) \
		docker compose -f ./docker/control-plane-dev/docker-compose.yaml
docker_compose_ci=docker compose -f ./docker/control-plane-ci/docker-compose.yaml
e2e_args=-tags=e2e_test -count=1 -timeout=45m \
	$(if $(E2E_PARALLEL),-parallel $(E2E_PARALLEL)) \
	$(if $(E2E_RUN),-run $(E2E_RUN)) \
	-args \
	$(if $(E2E_FIXTURE),-fixture $(E2E_FIXTURE)) \
	$(if $(filter 1,$(E2E_SKIP_CLEANUP)),-skip-cleanup) \
	$(if $(filter 1,$(E2E_DEBUG)),-debug) \
	$(if $(E2E_DEBUG_DIR),-debug-dir $(E2E_DEBUG_DIR))

cluster_test_args=-tags=cluster_test -count=1 -timeout=10m \
	$(if $(CLUSTER_TEST_PARALLEL),-parallel $(CLUSTER_TEST_PARALLEL)) \
	$(if $(CLUSTER_TEST_RUN),-run $(CLUSTER_TEST_RUN)) \
	-args \
	$(if $(filter 1,$(CLUSTER_TEST_SKIP_CLEANUP)),-skip-cleanup) \
	$(if $(filter 1,$(CLUSTER_TEST_SKIP_IMAGE_BUILD)),-skip-image-build) \
	$(if $(CLUSTER_TEST_IMAGE_TAG),-image-tag $(CLUSTER_TEST_IMAGE_TAG)) \
	$(if $(CLUSTER_TEST_DATA_DIR),-data-dir $(CLUSTER_TEST_DATA_DIR))

# Automatically adds junit output named after the rule, e.g.
# 'test-e2e-results.xml' in CI environment.
gotestsum=$(gobin)/gotestsum \
	$(if $(filter true,$(CI)),--junitfile $@-results.xml)

golangci-lint=$(gobin)/golangci-lint \
	$(if $(filter true,$(CI)),--output.junit-xml.path $@-results.xml)

.DEFAULT_GOAL := build

###########
# testing #
###########

.PHONY: test
test:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--rerun-fails=$(TEST_RERUN_FAILS) \
		--packages='./...'

.PHONY: test-etcd
test-etcd-lifecycle:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--rerun-fails=$(TEST_RERUN_FAILS) \
		--packages='./server/internal/etcd/...' \
		-- \
		-tags=etcd_lifecycle_test

.PHONY: test-workflows-backend
test-workflows-backend:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--rerun-fails=$(TEST_RERUN_FAILS) \
		--packages='./server/internal/workflows/backend/etcd/...' \
		-- \
		-tags=workflows_backend_test

.PHONY: test-ci
test-ci:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--junitfile test-results.xml \
		--rerun-fails=$(TEST_RERUN_FAILS) \
		--packages='./...' \
		-- \
		-tags=workflows_backend_test,etcd_lifecycle_test

.PHONY: test-e2e
test-e2e:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--format standard-verbose \
		--rerun-fails=$(TEST_RERUN_FAILS) \
		--rerun-fails-max-failures=4 \
		--packages='./e2e/...' \
		-- \
		$(e2e_args)

.PHONY: test-cluster
test-cluster:
	CONTROL_PLANE_VERSION="$(CONTROL_PLANE_VERSION)" \
	$(gotestsum) \
		--format-hide-empty-pkg \
		--format standard-verbose \
		--rerun-fails=$(TEST_RERUN_FAILS) \
		--rerun-fails-max-failures=4 \
		--packages='./clustertest/...' \
		-- \
		$(cluster_test_args)

.PHONY: lint
lint:
	$(golangci-lint) run ./...

# Exclude some dependencies from NOTICE.txt generation
# - github.com/pgEdge/control-plane is our own code
# - github.com/eclipse/paho.golang is licensed under EDL-1.0 explicitly in # NOTICES.txt.tmpl
.PHONY: licenses
licenses:
	GOOS=linux $(go-licenses) check ./...
	GOOS=linux $(go-licenses) report ./... \
	--ignore github.com/pgEdge/control-plane \
	--ignore github.com/eclipse/paho.golang \
	--template=NOTICE.txt.tmpl > NOTICE.txt

.PHONY: licenses-ci
licenses-ci: licenses
	@sort NOTICE.txt > ./licenses-ci-local.txt
	@git show HEAD:NOTICE.txt | sort > ./licenses-ci-upstream.txt
	@if ! diff ./licenses-ci-local.txt ./licenses-ci-upstream.txt > /dev/null; then \
		echo "Please commit the updated NOTICE.txt file via 'make licenses'."; \
		rm ./licenses-ci-local.txt ./licenses-ci-upstream.txt; \
		exit 1; \
	fi
	@rm ./licenses-ci-local.txt ./licenses-ci-upstream.txt
	@echo "NOTICE.txt is up to date."

.PHONY: ci
ci: test-ci lint licenses-ci

################
# e2e fixtures #
################

_fixture_extra_vars=$(if $(FIXTURE_CONTROL_PLANE_IMAGE),external_control_plane_image=$(FIXTURE_CONTROL_PLANE_IMAGE) ,)$(FIXTURE_EXTRA_VARS)

# Set to 'goreleaser-build' if no external image is specified
_fixture_goreleaser_build=$(if $(findstring external_control_plane_image,$(_fixture_extra_vars)),,goreleaser-build)

.PHONY: _deploy-%-fixture
_deploy-%-fixture: $(_fixture_goreleaser_build)
	VARIANT=$(FIXTURE_VARIANT) \
	EXTRA_VARS='$(_fixture_extra_vars)' \
	$(MAKE) -C e2e/fixtures \
		deploy-$*-machines \
		setup-$*-hosts \
		deploy-$*-control-plane

.PHONY: deploy-lima-fixture
deploy-lima-fixture: _deploy-lima-fixture

.PHONY: deploy-ec2-fixture
deploy-ec2-fixture: _deploy-ec2-fixture

.PHONY: _update-%-fixture
_update-%-fixture: $(_fixture_goreleaser_build)
	VARIANT=$(FIXTURE_VARIANT) \
	EXTRA_VARS='$(_fixture_extra_vars)' \
	$(MAKE) -C e2e/fixtures \
		deploy-$*-control-plane

.PHONY: update-lima-fixture
update-lima-fixture: _update-lima-fixture

.PHONY: update-ec2-fixture
update-ec2-fixture: _update-ec2-fixture

.PHONY: _reset-%-fixture
_reset-%-fixture: $(_fixture_goreleaser_build)
	VARIANT=$(FIXTURE_VARIANT) \
	EXTRA_VARS='$(_fixture_extra_vars)' \
	$(MAKE) -C e2e/fixtures \
		teardown-$*-control-plane \
		deploy-$*-control-plane

.PHONY: reset-lima-fixture
reset-lima-fixture: _reset-lima-fixture

.PHONY: reset-ec2-fixture
reset-ec2-fixture: _reset-ec2-fixture

.PHONY: _stop-%-fixture
_stop-%-fixture:
	VARIANT=$(FIXTURE_VARIANT) \
	EXTRA_VARS='$(_fixture_extra_vars)' \
	$(MAKE) -C e2e/fixtures \
		stop-$*-machines

.PHONY: stop-lima-fixture
stop-lima-fixture: _stop-lima-fixture

.PHONY: stop-ec2-fixture
stop-ec2-fixture: _stop-ec2-fixture

.PHONY: _teardown-%-fixture
_teardown-%-fixture:
	VARIANT=$(FIXTURE_VARIANT) \
	EXTRA_VARS='$(_fixture_extra_vars)' \
	$(MAKE) -C e2e/fixtures \
		teardown-$*-machines

.PHONY: teardown-lima-fixture
teardown-lima-fixture: _teardown-lima-fixture

.PHONY: teardown-ec2-fixture
teardown-ec2-fixture: _teardown-ec2-fixture

###############
# image build #
###############

.PHONY: start-local-registry
start-local-registry:
	docker service create --name registry --publish published=5000,target=5000 registry:2

.PHONY: buildx-init
buildx-init:
	docker buildx create \
		--name=$(buildx_builder) \
		--platform=linux/arm64,linux/amd64 \
		--config=$(buildx_config)

.PHONY: control-plane-images
control-plane-images:
	CONTROL_PLANE_IMAGE_REPO="$(CONTROL_PLANE_IMAGE_REPO)" \
	CONTROL_PLANE_VERSION="$(CONTROL_PLANE_VERSION)" \
	docker buildx bake \
		--builder $(buildx_builder) \
		--push \
		control_plane

.PHONY: goreleaser-build
goreleaser-build:
	GORELEASER_CURRENT_TAG=$(CONTROL_PLANE_VERSION) \
	$(goreleaser) build --snapshot --clean
	tar -C dist/control-plane_linux_amd64_v1 -c -z \
		-f dist/control-plane_$(CONTROL_PLANE_VERSION:v%=%)_linux_amd64.tar.gz \
		control-plane
	tar -C dist/control-plane_linux_arm64_v8.0 -c -z \
		-f dist/control-plane_$(CONTROL_PLANE_VERSION:v%=%)_linux_arm64.tar.gz \
		control-plane

goreleaser-test-release:
	GORELEASER_CURRENT_TAG=$(CONTROL_PLANE_VERSION) \
	$(goreleaser) release --skip=publish --snapshot --clean

###########
# release #
###########

.PHONY: changelog-entry
changelog-entry:
	$(changie) new

.PHONY: release
release:
ifeq ($(VERSION),)
	$(error VERSION must be set to trigger a release. )
endif
ifeq ($(shell uname -s),Darwin)
	sed -i '' 's/$(shell $(changie) latest)/$(VERSION)/g' $(shell find docs -name '*.md')
else
	sed -i 's/$(shell $(changie) latest)/$(VERSION)/g' $(shell find docs -name '*.md')
endif
	$(changie) batch $(VERSION)
	$(changie) merge
	$(changie) latest > api/version.txt
	$(MAKE) -C api generate
	cp CHANGELOG.md docs/changelog.md
	git checkout -b release/$(VERSION)
	git add api changes docs CHANGELOG.md mkdocs.yml
	git -c core.pager='' diff --staged
	git -c core.pager='' diff --staged --compact-summary
	@echo -n "Are you sure? [y/N] " && read ans && [ $${ans:-N} == y ]
	git commit -m "build(release): bump version to $(VERSION)"
	git push origin release/$(VERSION)
	git tag -a -F changes/$(VERSION).md $(VERSION)-rc.1
	git push origin $(VERSION)-rc.1
	@echo "Go to https://github.com/pgEdge/control-plane/compare/release/$(VERSION)?expand=1 to open the release PR."

.PHONY: major-release
major-release:
	$(MAKE) release VERSION=$(shell $(changie) next major)

.PHONY: minor-release
minor-release:
	$(MAKE) release VERSION=$(shell $(changie) next minor)

.PHONY: patch-release
patch-release:
	$(MAKE) release VERSION=$(shell $(changie) next patch)

.PHONY: print-next-versions
print-next-versions:
	@echo "Next major version: $(shell $(changie) next major)"
	@echo "Next minor version: $(shell $(changie) next minor)"
	@echo "Next patch version: $(shell $(changie) next patch)"

##################################
# docker compose dev environment #
##################################

.PHONY: build
build: dev-build

.PHONY: dev-build
dev-build: 
	GOOS=linux go build \
		-gcflags "all=-N -l" \
		-o docker/control-plane-dev/control-plane \
		$(shell pwd)/server

.PHONY: docker-swarm-init
docker-swarm-init:
ifneq ($(docker_swarm_state),active)
	@echo "Docker is not in swarm mode, running 'docker swarm init'..."
	docker swarm init
else
	@echo "Docker is already in swarm mode"
endif

.PHONY: docker-swarm-leave
docker-swarm-leave:
ifeq ($(docker_swarm_state),active)
	@echo "Docker is in swarm mode, running 'docker swarm leave --force'..."
	docker swarm leave --force
else
	@echo "Docker Swarm is already inactive"
endif

.PHONY: dev-watch
dev-watch: dev-build docker-swarm-init
	$(docker_compose_dev) build
	$(docker_compose_dev) up --watch

.PHONY: dev-detached
dev-detached: dev-build docker-swarm-init
	$(docker_compose_dev) build
	$(docker_compose_dev) up --detach --wait --wait-timeout 30

.PHONY: dev-down
dev-down:
	$(docker_compose_dev) down

.PHONY: dev-teardown
dev-teardown: dev-down
	# remove postgres and supported services
	ids=$$(docker service ls -q); \
	if [ -n "$$ids" ]; then \
		echo "$$ids" \
			| xargs docker service inspect \
			--format '{{.ID}} {{index .Spec.Labels "pgedge.component"}}' \
			| awk '$$2=="postgres" || $$2=="service" {print $$1}' \
			| xargs docker service rm; \
	fi
	docker network ls \
		--filter=scope=swarm \
		--format '{{ .Name }}' \
		| awk '$$1 ~ /-database$$/' \
		| xargs docker network rm
	rm -rf ./docker/control-plane-dev/data

.PHONY: api-docs
api-docs:
	WORKSPACE_DIR=$(shell pwd) DEBUG=0 docker compose -f ./docker/control-plane-dev/docker-compose.yaml up api-docs

#################################
# docker compose ci environment #
#################################

.PHONY: ci-compose-build
ci-compose-build: 
	GOOS=linux go build \
		-gcflags "all=-N -l" \
		-o docker/control-plane-ci/control-plane \
		$(shell pwd)/server

.PHONY: ci-compose-detached
ci-compose-detached: ci-compose-build docker-swarm-init
	$(docker_compose_ci) build
	$(docker_compose_ci) up --detach --wait --wait-timeout 30

.PHONY: ci-compose-down
ci-compose-down:
	$(docker_compose_ci) down

#################################
# 		  documentation 	    #
#################################
.PHONY: docs
docs:
	docker build -t control-plane-docs ./docs
	docker run --rm -it -p 8000:8000 -v ${PWD}:/docs control-plane-docs
