include ../../common.mk

# Overridable vars
LIMA_VARS_FILE ?= vars/lima.yaml
EC2_VARS_FILE ?= vars/ec2.yaml
VARIANT ?= large
EXTRA_VARS ?=

variant_vars_file=vars/$(VARIANT).yaml

################
# Lima targets #
################

ansible_playbook_lima=ansible-playbook \
	--extra-vars='@$(LIMA_VARS_FILE)' \
	--extra-vars='@$(variant_vars_file)' \
	--extra-vars='$(EXTRA_VARS)'

.PHONY: deploy-lima-machines
deploy-lima-machines:
	$(ansible_playbook_lima) deploy_lima_machines.yaml

.PHONY: stop-lima-machines
stop-lima-machines:
	$(ansible_playbook_lima) stop_machines.yaml

.PHONY: teardown-lima-machines
teardown-lima-machines:
	$(ansible_playbook_lima) teardown_machines.yaml

.PHONY: setup-lima-hosts
setup-lima-hosts:
	$(ansible_playbook_lima) \
		--inventory=outputs/lima.inventory.yaml \
		setup_hosts.yaml

.PHONY: deploy-lima-control-plane
deploy-lima-control-plane:
	$(ansible_playbook_lima) \
		--inventory=outputs/lima.inventory.yaml \
		--extra-vars='version=$(CONTROL_PLANE_VERSION:v%=%)' \
		deploy_control_plane.yaml

.PHONY: teardown-lima-control-plane
teardown-lima-control-plane:
	$(ansible_playbook_lima) \
		--inventory=outputs/lima.inventory.yaml \
		teardown_control_plane.yaml

###############
# EC2 targets #
###############

ansible_playbook_ec2=ansible-playbook \
	--extra-vars='@$(EC2_VARS_FILE)' \
	--extra-vars='@$(variant_vars_file)' \
	--extra-vars='$(EXTRA_VARS)'

.PHONY: deploy-ec2-machines
deploy-ec2-machines:
	$(ansible_playbook_ec2) deploy_ec2_machines.yaml

.PHONY: stop-ec2-machines
stop-ec2-machines:
	$(ansible_playbook_ec2) stop_machines.yaml

.PHONY: teardown-ec2-machines
teardown-ec2-machines:
	$(ansible_playbook_ec2) teardown_machines.yaml

.PHONY: setup-ec2-hosts
setup-ec2-hosts:
	$(ansible_playbook_ec2) \
		--inventory=outputs/ec2.inventory.yaml \
		setup_hosts.yaml

.PHONY: deploy-ec2-control-plane
deploy-ec2-control-plane:
	$(ansible_playbook_ec2) \
		--inventory=outputs/ec2.inventory.yaml \
		--extra-vars='version=$(CONTROL_PLANE_VERSION:v%=%)' \
		deploy_control_plane.yaml

.PHONY: teardown-ec2-control-plane
teardown-ec2-control-plane:
	$(ansible_playbook_ec2) \
		--inventory=outputs/ec2.inventory.yaml \
		teardown_control_plane.yaml
