# TODO: go 1.24 has official support for tools in go.mod:  https://tip.golang.org/doc/go1.24#tools
gobin=$(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

gotestsum=$(gobin)/gotestsum
golangcilint=$(gobin)/golangci-lint

.PHONY: install-tools
install-tools:
	go install gotest.tools/gotestsum@v1.12.0
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
