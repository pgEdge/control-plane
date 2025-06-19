include ../tools.mk

.PHONY: generate-v1
generate-v1:
	goa gen github.com/pgEdge/control-plane/api/apiv1/design -o apiv1
	$(yamlfmt) $(CURDIR)/v1/gen/http/*.yaml

.PHONY: generate
generate: generate-v1
