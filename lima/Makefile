ansible_playbook=ansible-playbook \
	--extra-vars='@vars.yaml'

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
