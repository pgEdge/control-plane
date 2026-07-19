FROM golang:1.26.5

ENV CGO_ENABLED=0

RUN go install github.com/go-delve/delve/cmd/dlv@latest

COPY ./entrypoint.sh /entrypoint.sh
COPY ./pgedge-control-plane /pgedge-control-plane

# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
# trivy:ignore:DS-0002 -- dev-only image; runs as root by design, same
# reasoning as the production Dockerfile (bind-mount ownership management).
ENTRYPOINT [ "/entrypoint.sh" ]
