first_gopath=$(firstword $(subst :, ,$(shell go env GOPATH)))
gobin=$(or $(shell go env GOBIN),$(first_gopath)/bin)

gotestsum=$(gobin)/gotestsum
golangcilint=$(gobin)/golangci-lint
goa=$(gobin)/goa
goreleaser=$(gobin)/goreleaser
changie=$(gobin)/changie
yamlfmt=$(gobin)/yamlfmt

.PHONY: install-tools
install-tools:
	go install gotest.tools/gotestsum@v1.12.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.5
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.2
	go install goa.design/goa/v3/cmd/goa@v3.19.1
	go install github.com/goreleaser/goreleaser/v2@v2.9.0
	go install github.com/anchore/syft/cmd/syft@v1.25.1
	go install github.com/miniscruff/changie@v1.21.1
	go install github.com/google/yamlfmt/cmd/yamlfmt@v0.17.0
