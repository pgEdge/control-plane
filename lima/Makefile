ansible_playbook=ansible-playbook \
	--extra-vars='@vars.yaml'

.PHONY: deploy
deploy:
	$(ansible_playbook) deploy.yaml

.PHONY: teardown
teardown:
	$(ansible_playbook) teardown.yaml
	rm -rf ./data

.PHONY: reset
reset:
	$(ansible_playbook) stop-dbs.yaml
	rm -rf ./data

.PHONY: build
build:
	GOOS=linux go build \
		-gcflags "all=-N -l" \
		-o ./pgedge-control-plane \
		$(shell pwd)/../server

.PHONY: fix-clocks
fix-clocks:
	ansible --become -i ./inventory.yaml all -m command -a 'chronyc -a makestep'

run: build fix-clocks
	LIMA_DIR=$(shell pwd) circusd ./circus.ini
