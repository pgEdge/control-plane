include tools.mk
include pgedge.mk

# Overridable vars
DEBUG ?= 0
LOG_LEVEL ?= info
DEV_IMAGE_REPO ?= ghcr.io/pgedge
CONTROL_PLANE_IMAGE_REPO ?= host.docker.internal:5000/control-plane
CONTROL_PLANE_VERSION ?= $(shell git describe --tags --abbrev=0 --match 'v*')
E2E_FIXTURE ?=
E2E_PARALLEL ?=
E2E_RUN ?=
E2E_SKIP_CLEANUP ?= 0

buildx_builder=$(if $(CI),"control-plane-ci","control-plane")
buildx_config=$(if $(CI),"./buildkit.ci.toml","./buildkit.toml")
docker_compose_dev=WORKSPACE_DIR=$(shell pwd) \
		DEBUG=$(DEBUG) \
		LOG_LEVEL=$(LOG_LEVEL) \
		DEV_IMAGE_REPO=$(DEV_IMAGE_REPO) \
		docker compose -f ./docker/control-plane-dev/docker-compose.yaml
docker_compose_ci=docker compose -f ./docker/control-plane-ci/docker-compose.yaml
e2e_args=-tags=e2e_test -count=1 -timeout=20m ./e2e/... \
	$(if $(E2E_PARALLEL),-parallel $(E2E_PARALLEL)) \
	$(if $(E2E_RUN),-run $(E2E_RUN)) \
	-args \
	$(if $(E2E_FIXTURE),-fixture $(E2E_FIXTURE)) \
	$(if $(filter 1,$(E2E_SKIP_CLEANUP)),-skip-cleanup)

###########
# testing #
###########

.PHONY: test
test:
	$(gotestsum) \
		--format-hide-empty-pkg \
		./...

.PHONY: test-etcd
test-etcd-lifecycle:
	$(gotestsum) \
		--format-hide-empty-pkg \
		-- \
		-tags=etcd_lifecycle_test \
		./server/internal/etcd/...

.PHONY: test-workflows-backend
test-workflows-backend:
	$(gotestsum) \
		--format-hide-empty-pkg \
		-- \
		-tags=workflows_backend_test \
		./server/internal/workflows/backend/etcd/...

.PHONY: test-ci
test-ci:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--junitfile test-results.xml \
		-- \
		-tags=workflows_backend_test,etcd_lifecycle_test \
		./...

.PHONY: test-e2e
test-e2e:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--format standard-verbose \
		-- \
		$(e2e_args)

.PHONY: test-e2e-ci
test-e2e-ci:
	$(gotestsum) \
		--format-hide-empty-pkg \
		--format standard-verbose \
		--junitfile e2e-test-results.xml \
		-- \
		$(e2e_args)

.PHONY: lint
lint:
	$(golangcilint) run ./...

.PHONY: lint-ci
lint-ci:
	$(golangcilint) run \
		--output.junit-xml.path lint-results.xml \
		./...

.PHONY: ci
ci: test-ci lint-ci

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
	tar -C dist --strip-components=1 -c -z \
		-f dist/control-plane_$(CONTROL_PLANE_VERSION:v%=%)_linux_amd64.tar.gz \
		control-plane_linux_amd64_v1
	tar -C dist --strip-components=1 -c -z \
		-f dist/control-plane_$(CONTROL_PLANE_VERSION:v%=%)_linux_arm64.tar.gz \
		control-plane_linux_arm64_v8.0

goreleaser-test-publish:
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
	$(changie) batch $(VERSION)
	$(changie) merge
	$(changie) latest > api/version.txt
	$(MAKE) -C api generate
	git checkout -b release/$(VERSION)
	git add api changes CHANGELOG.md
	git -c core.pager='' diff --staged
	git -c core.pager='' diff --staged --compact-summary
	@echo -n "Are you sure? [y/N] " && read ans && [ $${ans:-N} == y ]
	git commit -m "build(release): bump version to $(VERSION)"
	git push origin release/$(VERSION)
	git tag $(VERSION)-rc.1
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

.PHONY: dev-build
dev-build: 
	GOOS=linux go build \
		-gcflags "all=-N -l" \
		-o docker/control-plane-dev/control-plane \
		$(shell pwd)/server

.PHONY: docker-swarm-mode
docker-swarm-mode:
	@if [ "$$(docker info --format '{{.Swarm.LocalNodeState}}')" != "active" ]; then \
		echo "Docker is not in swarm mode, running 'docker swarm init'..."; \
		docker swarm init; \
	fi

.PHONY: dev-watch
dev-watch: dev-build docker-swarm-mode
	$(docker_compose_dev) build
	$(docker_compose_dev) up --watch

.PHONY: dev-detached
dev-detached: dev-build docker-swarm-mode
	$(docker_compose_dev) build
	$(docker_compose_dev) up --detach --wait --wait-timeout 30

.PHONY: dev-down
dev-down:
	$(docker_compose_dev) down

.PHONY: dev-teardown
dev-teardown: dev-down
	docker service ls \
		--filter=label=pgedge.component=postgres \
		--format '{{ .ID }}' \
		| xargs docker service rm
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
ci-compose-detached: ci-compose-build docker-swarm-mode
	$(docker_compose_ci) build
	$(docker_compose_ci) up --detach --wait --wait-timeout 30

.PHONY: ci-compose-down
ci-compose-down:
	$(docker_compose_ci) down

######################
# vm dev environment #
######################

.PHONY: vagrant-init
vagrant-init:
	@$(MAKE) vagrant-up
	ansible-playbook playbook.yaml

.PHONY: vagrant-up
vagrant-up:
	ansible-inventory --list | jq -r '.control_plane.hosts[]' | xargs -P3 -I {} vagrant up {}
	vagrant ssh-config > ./vagrant-ssh.cfg

.PHONY: vagrant-destroy
vagrant-destroy:
	vagrant destroy -f

.PHONY: ssh-1
ssh-1:
	ssh -F ./vagrant-ssh.cfg control-plane-1

.PHONY: ssh-2
ssh-2:
	ssh -F ./vagrant-ssh.cfg control-plane-2

.PHONY: ssh-3
ssh-3:
	ssh -F ./vagrant-ssh.cfg control-plane-3
