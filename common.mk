first_gopath=$(firstword $(subst :, ,$(shell go env GOPATH)))
gobin=$(or $(shell go env GOBIN),$(first_gopath)/bin)

gotestsum=$(gobin)/gotestsum
golangci-lint=$(gobin)/golangci-lint
goa=$(gobin)/goa
goreleaser=$(gobin)/goreleaser
changie=$(gobin)/changie
yamlfmt=$(gobin)/yamlfmt
go-licenses=$(gobin)/go-licenses

CHANGIE_LATEST = $(shell $(changie) latest)
# deferred simple variable expansion pattern:
# https://make.mad-scientist.net/deferred-simple-variable-expansion/
# The 'git fetch' and 'git describe' will return the latest tag if we're on the
# release branch. The 'echo' returns our fallback for every other branch,
# including main.
CONTROL_PLANE_VERSION ?= $(eval CONTROL_PLANE_VERSION := $$(shell git fetch --quiet --tags && git describe --tags --abbrev=0 --match '$$(CHANGIE_LATEST)*' 2>/dev/null || echo '$$(CHANGIE_LATEST)'))$(CONTROL_PLANE_VERSION)

.PHONY: install-tools
install-tools:
	go install gotest.tools/gotestsum@v1.13.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	go install goa.design/goa/v3/cmd/goa@v3.23.4
	# TODO: goreleaser v2.14+ requires Go 1.26+. GONOSUMDB bypasses a broken
	# deprecation check on the sum DB. Remove and bump goreleaser when we
	# upgrade to Go 1.26.
	GONOSUMDB=github.com/goreleaser/goreleaser go install github.com/goreleaser/goreleaser/v2@v2.13.3
	go install github.com/anchore/syft/cmd/syft@v1.40.0
	go install github.com/miniscruff/changie@v1.24.0
	go install github.com/google/yamlfmt/cmd/yamlfmt@v0.21.0
	go install github.com/google/go-licenses/v2@v2.0.1
