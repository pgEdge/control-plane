include tools.mk

CODACY_CODE ?= $(shell pwd)
DEBUG ?= 0
modules=$(shell go list -m -f '{{ .Dir }}' | awk -F '/' '{ print "./" $$NF "/..."  }')
module_src_files=$(shell go list -m -f '{{ .Dir }}' | xargs find -f)
aws_account_id=$(shell aws sts get-caller-identity --query 'Account' --output text)
terraform_backend=control-plane-terraform-$(aws_account_id)
terraform_dir=terraform/deployments/$(terraform_deployment)

# AWS accounts where we deploy ECR repositories
aws_account_id_jason=529820047909
aws_account_id_assets=583677930824

# Determine the name of the terraform deployment based on the  we're working with. This should
# match a directory name: ./terraform/pgedge/<name>.
ifeq ($(aws_account_id),$(aws_account_id_jason))
	terraform_deployment=jason
else ifeq ($(aws_account_id),$(aws_account_id_assets))
	terraform_deployment=assets
endif

.PHONY: test
test:
	$(gotestsum) \
		--format-hide-empty-pkg \
		$(modules)

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

.PHONY: docker-images
docker-images:
	docker buildx bake \
		--builder control-plane \
		--push \
		pgedge

.PHONY: local-docker-images
local-docker-images:
	IMAGE_REPO_HOST=host.docker.internal:5000 docker buildx bake \
		--builder control-plane \
		--push \
		pgedge

.PHONY: start-local-registry
start-local-registry:
	docker run -d -p 5000:5000 --restart=always --name registry registry:2

.PHONY: buildx-init
buildx-init:
	docker buildx create \
		--name=control-plane \
		--platform=linux/arm64,linux/amd64 \
		--config=./buildkit.toml

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

.PHONY: codacy
codacy:
	docker run \
		--rm=true \
		--env CODACY_CODE="$(CODACY_CODE)" \
		--volume /var/run/docker.sock:/var/run/docker.sock \
		--volume "$(CODACY_CODE)":"$(CODACY_CODE)" \
		--volume /tmp:/tmp \
		codacy/codacy-analysis-cli \
			analyze

.PHONY: dev-build
dev-build: docker/control-plane-dev/control-plane

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

.PHONY: terraform-backend
terraform-backend:
	@if ! aws s3api head-bucket --bucket $(terraform_backend) > /dev/null 2>&1; then \
		aws s3 mb s3://$(terraform_backend); \
	fi

.PHONY: terraform-init
terraform-init: terraform-backend
ifeq ($(terraform_deployment),)
	$(error Unknown terraform deployment for AWS account id $(aws_account_id))
endif
	terraform -chdir=$(terraform_dir) init

.PHONY: terraform-deploy
terraform-deploy: terraform-init
	terraform -chdir=$(terraform_dir) apply -auto-approve
