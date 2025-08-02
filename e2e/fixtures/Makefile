# Overridable vars
CONTROL_PLANE_VERSION ?= $(shell git describe --tags --abbrev=0 --match 'v*')
VARS_FILE ?= vars/lima_rocky.yaml
EXTRA_VARS ?=

ansible_playbook=ansible-playbook \
	--extra-vars='@$(VARS_FILE)' \
	--extra-vars='$(EXTRA_VARS)'

.PHONY: deploy-machines
deploy-machines:
	$(ansible_playbook) deploy_machines.yaml

.PHONY: teardown-machine
teardown-machines:
	$(ansible_playbook) teardown_machines.yaml

.PHONY: setup-hosts
setup-hosts:
	$(ansible_playbook) \
		--inventory=outputs/inventory.yaml \
		setup_hosts.yaml

.PHONY: deploy-control-plane
deploy-control-plane:
	$(ansible_playbook) \
		--inventory=outputs/inventory.yaml \
		--extra-vars='version=$(CONTROL_PLANE_VERSION:v%=%)' \
		deploy_control_plane.yaml

.PHONY: deploy-control-plane
teardown-control-plane:
	$(ansible_playbook) \
		--inventory=outputs/inventory.yaml \
		teardown_control_plane.yaml
