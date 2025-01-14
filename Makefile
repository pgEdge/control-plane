include tools.mk

modules=$(shell go list -m -f '{{ .Dir }}' | awk -F '/' '{ print "./" $$NF "/..."  }')

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
		--out-format junit-xml \
		$(modules) > lint-results.xml

.PHONY: ci
ci: test-ci lint-ci
