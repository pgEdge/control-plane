include tools.mk

# Overridable vars
DEBUG ?= 0
LOG_LEVEL ?= info
CONTROL_PLANE_IMAGE_REPO ?= host.docker.internal:5000/control-plane
CONTROL_PLANE_VERSION ?= $(shell git describe --tags --abbrev=0)
PGEDGE_IMAGE_REPO ?= host.docker.internal:5000/pgedge
PACKAGE_REPO_BASE_URL ?= http://pgedge-529820047909-yum.s3-website.us-east-2.amazonaws.com
PACKAGE_RELEASE_CHANNEL ?= dev

modules=$(shell go list -m -f '{{ .Dir }}' | awk -F '/' '{ print "./" $$NF "/..."  }')
module_src_files=$(shell go list -m -f '{{ .Dir }}' | xargs find -f)
buildx_builder=$(if $(CI),"control-plane-ci","control-plane")
buildx_config=$(if $(CI),"./buildkit.ci.toml","./buildkit.toml")
docker_compose_dev=WORKSPACE_DIR=$(shell pwd) \
		DEBUG=$(DEBUG) \
		LOG_LEVEL=$(LOG_LEVEL) \
		docker compose -f ./docker/control-plane-dev/docker-compose.yaml

###########
# testing #
###########

.PHONY: test
test:
	$(gotestsum) \
		--format-hide-empty-pkg \
		$(modules)

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
		$(modules)

.PHONY: lint
lint:
	$(golangcilint) run $(modules)

.PHONY: lint-ci
lint-ci:
	$(golangcilint) run \
		--output.junit-xml.path lint-results.xml \
		$(modules)

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

.PHONY: pgedge-images
pgedge-images:
	PGEDGE_IMAGE_REPO="$(PGEDGE_IMAGE_REPO)" \
	PACKAGE_REPO_BASE_URL="$(PACKAGE_REPO_BASE_URL)" \
	PACKAGE_RELEASE_CHANNEL="$(PACKAGE_RELEASE_CHANNEL)" \
	docker buildx bake \
		--builder $(buildx_builder) \
		--push \
		pgedge

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
	goreleaser build --snapshot --clean
	tar -C dist --strip-components=1 -c -z \
		-f dist/control-plane_$(CONTROL_PLANE_VERSION:v%=%)_linux_amd64.tar.gz \
		control-plane_linux_amd64_v1
	tar -C dist --strip-components=1 -c -z \
		-f dist/control-plane_$(CONTROL_PLANE_VERSION:v%=%)_linux_arm64.tar.gz \
		control-plane_linux_arm64_v8.0

###########
# release #
###########

.PHONY: changelog-entry
changelog-entry:
	$(changie) new

.PHONY: version-tags
version-tags:
ifeq ($(TAG),)
	$(error TAG must be set. )
endif
ifeq ($(CHANGELOG),)
	git tag $(TAG)
else
	git tag -a -F $(CHANGELOG) $(TAG)
endif
	git push origin $(TAG)
	@for module in $(shell go list -m | xargs basename); do \
		module_tag="$${module}/$(TAG)"; \
		echo "creating and pushing module tag $${module_tag}"; \
		git tag $${module_tag}; \
		git push origin $${module_tag}; \
	done

.PHONY: release
release:
ifeq ($(VERSION),)
	$(error VERSION must be set to trigger a release. )
endif
	$(changie) batch $(VERSION)
	$(changie) merge
	$(changie) latest > api/design/version.txt
	$(MAKE) -C api generate
	git checkout -b release/$(VERSION)
	git add api changes CHANGELOG.md
	git -c core.pager='' diff --staged
	git -c core.pager='' diff --staged --compact-summary
	@echo -n "Are you sure? [y/N] " && read ans && [ $${ans:-N} == y ]
	git commit -m "build(release): bump version to $(VERSION)"
	git push origin release/$(VERSION)
	@$(MAKE) version-tags TAG=$(VERSION)-rc.1
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
dev-build: docker/control-plane-dev/control-plane

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

.PHONY: down
dev-down:
	$(docker_compose_dev) down

docker/control-plane-dev/control-plane: $(module_src_files)
	GOOS=linux go build -gcflags "all=-N -l" -o $@ $(shell pwd)/server

.PHONY: api-docs
api-docs:
	WORKSPACE_DIR=$(shell pwd) DEBUG=0 docker compose -f ./docker/control-plane-dev/docker-compose.yaml up api-docs

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
