# Control Plane HTTP API Test Makefile
# Usage:
#   make launch-vms      - Launch AWS EC2 instances
#   make setup-cp        - Install Docker, Swarm, and deploy Control Plane
#   make run-tests       - Run HTTP API tests
#   make all             - Run complete pipeline

SHELL := /bin/bash
ANSIBLE_DIR := /Users/usman/pge/repos/ansible/launch_aws_setup
CONTROL_PLANE_DIR := /Users/usman/pge/repos/control-plane

.PHONY: all launch-vms setup-cp run-tests help show-nodes health clean

# Complete pipeline
all: launch-vms setup-cp run-tests

# 1) Launch AWS VMs only
launch-vms:
	@echo "==> Launching AWS VMs..."
	cd $(ANSIBLE_DIR) && ansible-playbook site.yaml
	@echo "==> VMs launched and configured"

# 2) Setup Docker, Swarm, and deploy Control Plane (for existing VMs)
setup-cp:
	@echo "==> Setting up Docker Swarm and Control Plane..."
	cd $(ANSIBLE_DIR) && ansible-playbook site.yaml
	@echo "==> Control Plane deployed"

# 3) Run HTTP API tests
run-tests:
	@echo "==> Running HTTP API tests..."
	cd $(CONTROL_PLANE_DIR) && go test -v -tags=http_apitest ./http-apitest/tests/...
	@echo "==> Tests completed"

# Run tests with verbose output
run-tests-verbose:
	@echo "==> Running HTTP API tests (verbose)..."
	cd $(CONTROL_PLANE_DIR) && go test -v -tags=http_apitest -count=1 ./http-apitest/tests/...

# Show current nodes configuration
show-nodes:
	@echo "==> Current nodes configuration:"
	@cat $(CONTROL_PLANE_DIR)/http-apitest/tests/nodes_config.json

# Health check all nodes
health:
	@echo "==> Checking Control Plane health on all nodes..."
	@if [ -f $(CONTROL_PLANE_DIR)/http-apitest/tests/nodes_config.json ]; then \
		for ip in $$(cat $(CONTROL_PLANE_DIR)/http-apitest/tests/nodes_config.json | grep -oE '"public_ip":\s*"[^"]+"' | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'); do \
			echo -n "$$ip: "; \
			curl -s -w "%{http_code}" -o /dev/null http://$$ip:3000/v1/health 2>/dev/null || echo "UNREACHABLE"; \
			echo ""; \
		done; \
	else \
		echo "No nodes configured"; \
	fi

# SSH to first node
ssh-node1:
	@ip=$$(cat $(CONTROL_PLANE_DIR)/http-apitest/tests/nodes_config.json | grep -oE '"public_ip":\s*"[^"]+"' | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'); \
	echo "Connecting to $$ip..."; \
	ssh rocky@$$ip

# Redeploy Control Plane only (update redeploy_cp.yaml with IPs first)
redeploy:
	@echo "==> Redeploying Control Plane..."
	cd $(ANSIBLE_DIR) && ansible-playbook redeploy_cp.yaml
	@echo "==> Control Plane redeployed"

# Clean test cache
clean:
	@echo "==> Cleaning test cache..."
	cd $(CONTROL_PLANE_DIR) && go clean -testcache
	@echo "==> Done"

# Help
help:
	@echo "Control Plane HTTP API Test Infrastructure"
	@echo ""
	@echo "Usage:"
	@echo "  make all              - Run complete pipeline (launch VMs + setup CP + run tests)"
	@echo "  make launch-vms       - Launch AWS EC2 instances"
	@echo "  make setup-cp         - Install Docker, Swarm, deploy Control Plane"
	@echo "  make run-tests        - Run HTTP API tests"
	@echo "  make run-tests-verbose - Run tests with verbose output"
	@echo "  make show-nodes       - Show current nodes configuration"
	@echo "  make health           - Check Control Plane health on all nodes"
	@echo "  make ssh-node1        - SSH to first node"
	@echo "  make redeploy         - Redeploy Control Plane only"
	@echo "  make clean            - Clean test cache"
	@echo "  make help             - Show this help"
	@echo ""
	@echo "nodes_config.json format:"
	@echo '  {"nodes": [{"public_ip": "x.x.x.x", "private_ip": "172.31.x.x"}, ...]}'
