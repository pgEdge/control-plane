include tools.mk

# Overridable vars
DEBUG ?= 0
CONTROL_PLANE_IMAGE_REPO ?= host.docker.internal:5000/control-plane
CONTROL_PLANE_VERSION ?=
PGEDGE_IMAGE_REPO ?= host.docker.internal:5000/pgedge
PACKAGE_REPO_BASE_URL ?= http://pgedge-529820047909-yum.s3-website.us-east-2.amazonaws.com
PACKAGE_RELEASE_CHANNEL ?= dev

modules=$(shell go list -m -f '{{ .Dir }}' | awk -F '/' '{ print "./" $$NF "/..."  }')
module_src_files=$(shell go list -m -f '{{ .Dir }}' | xargs find -f)
buildx_builder=$(if $(CI),"control-plane-ci","control-plane")
buildx_config=$(if $(CI),"./buildkit.ci.toml","./buildkit.toml")

###########
# testing #
###########

.PHONY: test
test:
	$(gotestsum) \
		--format-hide-empty-pkg \
		$(modules)

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
		-tags=workflows_backend_test \
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
	docker run -d -p 5000:5000 --restart=always --name registry registry:2

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
	$(changie) latest > api/design/version.txt
	$(MAKE) -C api generate
	git checkout -b release/$(VERSION)
	git add api changes CHANGELOG.md
	git -c core.pager='' diff --staged ':(exclude)api/gen/**.json'
	git -c core.pager='' diff --staged --compact-summary
	@echo -n "Are you sure? [y/N] " && read ans && [ $${ans:-N} == y ]
	git commit -m "chore: bump version to $(VERSION)"
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
dev-build: docker/control-plane-dev/control-plane

.PHONY: docker-swarm-mode
docker-swarm-mode:
	@if [ "$$(docker info --format '{{.Swarm.LocalNodeState}}')" != "active" ]; then \
		echo "Docker is not in swarm mode, running 'docker swarm init'..."; \
		docker swarm init; \
	fi

.PHONY: dev-watch
dev-watch: dev-build docker-swarm-mode
	WORKSPACE_DIR=$(shell pwd) docker compose -f ./docker/control-plane-dev/docker-compose.yaml build
	WORKSPACE_DIR=$(shell pwd) DEBUG=$(DEBUG) docker compose -f ./docker/control-plane-dev/docker-compose.yaml up --watch

docker/control-plane-dev/control-plane: $(module_src_files)
	GOOS=linux go build -gcflags "all=-N -l" -o $@ $(shell pwd)/server

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
