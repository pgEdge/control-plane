# TODO: go 1.24 has official support for tools in go.mod:  https://tip.golang.org/doc/go1.24#tools
gobin=$(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

gotestsum=$(gobin)/gotestsum
golangcilint=$(gobin)/golangci-lint
goa=$(gobin)/goa
goreleaser=$(gobin)/goreleaser

.PHONY: install-tools
install-tools:
	go install gotest.tools/gotestsum@v1.12.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.5
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.2
	go install goa.design/goa/v3/cmd/goa@v3.19.1
	go install github.com/goreleaser/goreleaser/v2@latest
	go install github.com/anchore/syft/cmd/syft@v1.25.1
