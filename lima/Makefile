DEV_LIMA_OS ?= rocky-9
DEV_LIMA_USE_STAGING_PACKAGES ?= false
# Spock major installed on the fixture (e.g. "50" or "60"). Spock major
# packages conflict with each other at the package-manager level (a host can
# only have one Spock major installed per Postgres major), so this selects
# one for the whole fixture rather than installing every major side-by-side.
DEV_LIMA_SPOCK_MAJOR ?= 50

ansible_playbook=ansible-playbook \
	--extra-vars='@vars.yaml' \
	--extra-vars='os=$(DEV_LIMA_OS)' \
	--extra-vars='use_staging_packages=$(DEV_LIMA_USE_STAGING_PACKAGES)' \
	--extra-vars='spock_major=$(DEV_LIMA_SPOCK_MAJOR)'

.PHONY: quit
quit:
	[ -e ./circus.pid ] && circusctl --timeout 2 quit 2>/dev/null || true
	rm -f ./circus.pid

.PHONY: deploy
deploy:
	$(ansible_playbook) deploy.yaml

.PHONY: teardown
teardown: quit
	$(ansible_playbook) teardown.yaml
	rm -rf ./data

.PHONY: reset
reset: quit
	$(ansible_playbook) stop-dbs.yaml
	rm -rf ./data

.PHONY: build
build:
	CGO_ENABLED=0 GOOS=linux go build \
		-gcflags "all=-N -l" \
		-o ./pgedge-control-plane \
		$(shell pwd)/../server

.PHONY: fix-clocks
fix-clocks:
	ansible --become -i ./inventory.yaml all -m command -a 'chronyc -a makestep'

.PHONY: run
run: quit build fix-clocks
	LIMA_DIR=$(shell pwd) circusd --pidfile ./circus.pid ./circus.ini
