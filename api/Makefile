include ../tools.mk

.PHONY: generate-v1
generate-v1:
	goa gen github.com/pgEdge/control-plane/api/v1/design -o v1

.PHONY: generate
generate: generate-v1
